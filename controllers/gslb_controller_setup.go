package controllers

import (
	"context"
	"fmt"
	"strconv"

	k8gbv1beta1 "github.com/k8gb-io/k8gb/api/v1beta1"
	"github.com/k8gb-io/k8gb/controllers/depresolver"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	externaldns "sigs.k8s.io/external-dns/endpoint"
)

// SetupWithManager configures controller manager
func (r *GslbReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Figure out Gslb resource name to Reconcile when non controlled Name is updated

	endpointMapHandler := handler.EnqueueRequestsFromMapFunc(
		func(a client.Object) []reconcile.Request {
			gslbList := &k8gbv1beta1.GslbList{}
			opts := []client.ListOption{
				client.InNamespace(a.GetNamespace()),
			}
			c := mgr.GetClient()
			err := c.List(context.TODO(), gslbList, opts...)
			if err != nil {
				log.Info().Msg("Can't fetch gslb objects")
				return nil
			}
			gslbName := ""
			for _, gslb := range gslbList.Items {
				for _, rule := range gslb.Spec.Ingress.Rules {
					for _, path := range rule.HTTP.Paths {
						if path.Backend.Service != nil && path.Backend.Service.Name == a.GetName() {
							gslbName = gslb.Name
						}
					}
				}
			}
			if len(gslbName) > 0 {
				return []reconcile.Request{
					{NamespacedName: types.NamespacedName{
						Name:      gslbName,
						Namespace: a.GetNamespace(),
					}},
				}
			}
			return nil
		})

	ingressMapHandler := handler.EnqueueRequestsFromMapFunc(
		func(a client.Object) []reconcile.Request {
			annotations := a.GetAnnotations()
			log.Info().Msgf("XXXXX INGRES %s %v", a.GetName(), annotations)
			if annotationValue, found := annotations[strategyAnnotation]; found {
				c := mgr.GetClient()
				r.createGSLBFromIngress(c, a, strategyAnnotation, annotationValue)
			}
			return nil
		})

	return ctrl.NewControllerManagedBy(mgr).
		For(&k8gbv1beta1.Gslb{}).
		Owns(&netv1.Ingress{}).
		Owns(&externaldns.DNSEndpoint{}).
		Watches(&source.Kind{Type: &corev1.Endpoints{}}, endpointMapHandler).
		Watches(&source.Kind{Type: &netv1.Ingress{}}, ingressMapHandler).
		Complete(r)
}

func (r *GslbReconciler) createGSLBFromIngress(c client.Client, a client.Object, annotationKey, strategy string) {
	log.Debug().
		Str("annotation", fmt.Sprintf("(%s:%s)", annotationKey, strategy)).
		Str("ingress", a.GetName()).
		Msg("Detected strategy annotation on ingress")
	ingressToReuse := &netv1.Ingress{}
	err := c.Get(context.Background(), client.ObjectKey{
		Namespace: a.GetNamespace(),
		Name:      a.GetName(),
	}, ingressToReuse)
	if err != nil {
		log.Info().
			Str("ingress", a.GetName()).
			Msg("Ingress does not exist anymore. Skipping Glsb creation...")
		return
	}
	gslbExisting := &k8gbv1beta1.Gslb{}
	gslbExistErr := c.Get(context.Background(), client.ObjectKey{
		Namespace: a.GetNamespace(),
		Name:      a.GetName(),
	}, gslbExisting)

	gslbEmpty := &k8gbv1beta1.Gslb{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   a.GetNamespace(),
			Name:        a.GetName(),
			Annotations: a.GetAnnotations(),
		},
		Spec: k8gbv1beta1.GslbSpec{
			Ingress: k8gbv1beta1.FromV1IngressSpec(ingressToReuse.Spec),
			Strategy: k8gbv1beta1.Strategy{
				Type: strategy,
			},
		},
	}
	log.Info().Msgf("XXXXX GSLBEmpty1 %s %v", gslbEmpty.Name, gslbEmpty.Spec.Strategy)
	annotationToInt := func(k string, v string) int {
		intValue, err := strconv.Atoi(v)
		if err != nil {
			log.Err(err).
				Str("annotationKey", k).
				Str("annotationValue", v).
				Msg("Can't parse annotation value to int")
		}
		return intValue
	}

	log.Info().Msgf("XXXXX ANNOTATIONS: %v", a.GetAnnotations())
	for annotationKey, annotationValue := range a.GetAnnotations() {
		switch annotationKey {
		case dnsTTLSecondsAnnotation:
			gslbEmpty.Spec.Strategy.DNSTtlSeconds = annotationToInt(annotationKey, annotationValue)
		case splitBrainThresholdSecondsAnnotation:
			gslbEmpty.Spec.Strategy.SplitBrainThresholdSeconds = annotationToInt(annotationKey, annotationValue)
		}
	}
	log.Info().Msgf("XXXXX GSLBEmpty2 %s %v, ANNOTATIONS: %v", gslbEmpty.Name, gslbEmpty.Spec.Strategy, a)
	if strategy == depresolver.FailoverStrategy {
		for annotationKey, annotationValue := range a.GetAnnotations() {
			if annotationKey == primaryGeoTagAnnotation {
				gslbEmpty.Spec.Strategy.PrimaryGeoTag = annotationValue
			}
		}
		if gslbEmpty.Spec.Strategy.PrimaryGeoTag == "" {
			log.Info().
				Str("annotation", primaryGeoTagAnnotation).
				Str("gslb", gslbEmpty.Name).
				Msg("Annotation is missing, skipping Gslb creation...")
			return
		}
	}
	log.Info().Msgf("XXXXX GSLBEmpty3 %s %v", gslbEmpty.Name, gslbEmpty.Spec.Strategy)
	err = controllerutil.SetControllerReference(ingressToReuse, gslbEmpty, r.Scheme)
	if err != nil {
		log.Err(err).
			Str("ingress", ingressToReuse.Name).
			Str("gslb", gslbEmpty.Name).
			Msg("Cannot set the Ingress as the owner of the Gslb")
	}

	log.Info().
		Str("gslb", gslbEmpty.Name).
		Msg("XXXXX Creating new Gslb out of Ingress annotation")
	if errors.IsNotFound(gslbExistErr) {
		err = c.Create(context.Background(), gslbEmpty)
		if err != nil {
			log.Err(err).Msg("XXXXX Glsb creation failed")
		}
	} else {
		log.Info().Msgf("XXXXX Glsb update0: %s %v", gslbExisting.Name, gslbExisting.Spec)
		annotations := a.GetAnnotations()
		delete(annotations, "kubectl.kubernetes.io/last-applied-configuration")
		gslbExisting.Annotations = annotations
		gslbExisting.Spec = gslbEmpty.Spec
		log.Info().Msgf("XXXXX Glsb update1: %s %v", gslbExisting.Name, gslbExisting.Spec)
		err = c.Update(context.Background(), gslbExisting)
		if err != nil {
			log.Err(err).Msg("XXXXX Glsb update failed")
		}
	}
	log.Info().Msgf("XXXXX GSLBExists %s %v", gslbExisting.Name, gslbExisting.Spec.Strategy)
}
