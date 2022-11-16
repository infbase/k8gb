package controllers

/*
Copyright 2022 The k8gb Contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

Generated by GoLic, for more details see: https://github.com/AbsaOSS/golic
*/

import (
	"context"
	"fmt"

	"github.com/k8gb-io/k8gb/controllers/providers/metrics"

	k8gbv1beta1 "github.com/k8gb-io/k8gb/api/v1beta1"
	"github.com/k8gb-io/k8gb/controllers/depresolver"
	"github.com/k8gb-io/k8gb/controllers/internal/utils"
	"github.com/k8gb-io/k8gb/controllers/logging"
	"github.com/k8gb-io/k8gb/controllers/providers/dns"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GslbReconciler reconciles a Gslb object
type GslbReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	Config      *depresolver.Config
	DepResolver depresolver.GslbResolver
	DNSProvider dns.Provider
	Tracer      trace.Tracer
}

const (
	gslbFinalizer                        = "k8gb.absa.oss/finalizer"
	primaryGeoTagAnnotation              = "k8gb.io/primary-geotag"
	strategyAnnotation                   = "k8gb.io/strategy"
	dnsTTLSecondsAnnotation              = "k8gb.io/dns-ttl-seconds"
	splitBrainThresholdSecondsAnnotation = "k8gb.io/splitbrain-threshold-seconds"
)

var log = logging.Logger()

var m = metrics.Metrics()

// +kubebuilder:rbac:groups=k8gb.absa.oss,resources=gslbs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=k8gb.absa.oss,resources=gslbs/status,verbs=get;update;patch

// Reconcile runs main reconiliation loop
func (r *GslbReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx, span := r.Tracer.Start(ctx, "Reconcile")
	defer span.End()

	result := utils.NewReconcileResultHandler(r.Config.ReconcileRequeueSeconds)
	// Fetch the Gslb instance
	gslb := &k8gbv1beta1.Gslb{}
	err := r.Get(ctx, req.NamespacedName, gslb)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return result.Stop()
		}
		m.IncrementError(gslb)
		return result.RequeueError(fmt.Errorf("error reading the object (%s)", err))
	}

	//err = r.DepResolver.ResolveGslbSpec(ctx, gslb, r.Client)
	//if err != nil {
	//	m.IncrementError(gslb)
	//	return result.RequeueError(fmt.Errorf("resolving spec (%s)", err))
	//}
	log.Debug().
		Str("gslb", gslb.Name).
		Interface("strategy", gslb.Spec.Strategy).
		Msg("Resolved strategy")
	// == Finalizer business ==

	// Check if the Gslb instance is marked to be deleted, which is
	// indicated by the deletion timestamp being set.
	isGslbMarkedToBeDeleted := gslb.GetDeletionTimestamp() != nil
	if isGslbMarkedToBeDeleted {
		// For the legacy reasons, delete all finalizers that corresponds with the slice
		// see: https://sdk.operatorframework.io/docs/upgrading-sdk-version/v1.4.0/#change-your-operators-finalizer-names
		_, fSpan := r.Tracer.Start(ctx, "finalize")
		for _, f := range []string{gslbFinalizer, "finalizer.k8gb.absa.oss"} {
			if contains(gslb.GetFinalizers(), f) {
				// Run finalization logic for gslbFinalizer. If the
				// finalization logic fails, don't remove the finalizer so
				// that we can retry during the next reconciliation.
				if err := r.finalizeGslb(gslb); err != nil {
					fSpan.RecordError(err)
					fSpan.SetStatus(codes.Error, err.Error())
					return result.RequeueError(err)
				}

				// Remove gslbFinalizer. Once all finalizers have been
				// removed, the object will be deleted.
				gslb.SetFinalizers(remove(gslb.GetFinalizers(), f))
				err := r.Update(ctx, gslb)
				if err != nil {
					fSpan.RecordError(err)
					fSpan.SetStatus(codes.Error, err.Error())
					return result.RequeueError(err)
				}
			}

		}
		fSpan.End()
		log.Info().Msg("reconciler exit")
		return result.Stop()
	}

	// Add finalizer for this CR
	if !contains(gslb.GetFinalizers(), gslbFinalizer) {
		if err := r.addFinalizer(gslb); err != nil {
			m.IncrementError(gslb)
			return result.RequeueError(err)
		}
	}

	// == Ingress ==========
	//ingress, err := r.gslbIngress(gslb)
	//if err != nil {
	//	m.IncrementError(gslb)
	//	return result.RequeueError(err)
	//}
	//
	//err = r.saveIngress(gslb, ingress)
	//if err != nil {
	//	m.IncrementError(gslb)
	//	return result.RequeueError(err)
	//}

	// == external-dns dnsendpoints CRs ==
	dnsEndpoint, err := r.gslbDNSEndpoint(gslb)
	if err != nil {
		m.IncrementError(gslb)
		return result.RequeueError(err)
	}

	_, s := r.Tracer.Start(ctx, "SaveDNSEndpoint")
	err = r.DNSProvider.SaveDNSEndpoint(gslb, dnsEndpoint)
	if err != nil {
		m.IncrementError(gslb)
		return result.RequeueError(err)
	}
	s.End()

	// == handle delegated zone in Edge DNS
	_, szd := r.Tracer.Start(ctx, "CreateZoneDelegationForExternalDNS")
	err = r.DNSProvider.CreateZoneDelegationForExternalDNS(gslb)
	if err != nil {
		log.Err(err).Msg("Unable to create zone delegation")
		m.IncrementError(gslb)
		return result.Requeue()
	}
	szd.End()

	// == Status =
	err = r.updateGslbStatus(gslb, dnsEndpoint)
	if err != nil {
		m.IncrementError(gslb)
		return result.RequeueError(err)
	}

	// == Finish ==========
	// Everything went fine, requeue after some time to catch up
	// with external Gslb status
	// TODO: potentially enhance with smarter reaction to external Event
	m.IncrementReconciliation(gslb)
	return result.Requeue()
}
