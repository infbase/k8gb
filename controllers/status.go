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
	"regexp"
	"strings"

	k8gbv1beta1 "github.com/k8gb-io/k8gb/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	externaldns "sigs.k8s.io/external-dns/endpoint"
)

func (r *GslbReconciler) updateGslbStatus(gslb *k8gbv1beta1.Gslb, ep *externaldns.DNSEndpoint) error {
	var err error

	gslb.Status.ServiceHealth, err = r.getServiceHealthStatus(gslb)
	if err != nil {
		return err
	}

	m.UpdateIngressHostsPerStatusMetric(gslb, gslb.Status.ServiceHealth)

	gslb.Status.HealthyRecords, err = r.getHealthyRecords(gslb)
	if err != nil {
		return err
	}

	gslb.Status.GeoTag = r.Config.ClusterGeoTag
	gslb.Status.Hosts = r.hostsToCSV(gslb)

	m.UpdateHealthyRecordsMetric(gslb, gslb.Status.HealthyRecords)

	m.UpdateEndpointStatus(ep)

	return newMapper(r.Client).updateGslbStatus(gslb)
}

func (r *GslbReconciler) getServiceHealthStatus(gslb *k8gbv1beta1.Gslb) (map[string]k8gbv1beta1.HealthStatus, error) {
	serviceHealth := make(map[string]k8gbv1beta1.HealthStatus)
	for _, rule := range gslb.Spec.Ingress.Rules {
		for _, path := range rule.HTTP.Paths {
			if path.Backend.Service == nil || path.Backend.Service.Name == "" {
				log.Warn().
					Str("gslb", gslb.Name).
					Interface("service", path.Backend.Service).
					Msg("Malformed service definition")
				serviceHealth[rule.Host] = k8gbv1beta1.NotFound
				continue
			}
			service := &corev1.Service{}
			finder := client.ObjectKey{
				Namespace: gslb.Namespace,
				Name:      path.Backend.Service.Name,
			}
			err := r.Get(context.TODO(), finder, service)
			if err != nil {
				if errors.IsNotFound(err) {
					serviceHealth[rule.Host] = k8gbv1beta1.NotFound
					continue
				}
				return serviceHealth, err
			}

			endpoints := &corev1.Endpoints{}

			nn := types.NamespacedName{
				Name:      path.Backend.Service.Name,
				Namespace: gslb.Namespace,
			}

			err = r.Get(context.TODO(), nn, endpoints)
			if err != nil {
				return serviceHealth, err
			}

			serviceHealth[rule.Host] = k8gbv1beta1.Unhealthy
			if len(endpoints.Subsets) > 0 {
				for _, subset := range endpoints.Subsets {
					if len(subset.Addresses) > 0 {
						serviceHealth[rule.Host] = k8gbv1beta1.Healthy
					}
				}
			}
		}
	}
	return serviceHealth, nil
}

func (r *GslbReconciler) getHealthyRecords(gslb *k8gbv1beta1.Gslb) (map[string][]string, error) {

	dnsEndpoint := &externaldns.DNSEndpoint{}

	nn := types.NamespacedName{
		Name:      gslb.Name,
		Namespace: gslb.Namespace,
	}

	err := r.Get(context.TODO(), nn, dnsEndpoint)
	if err != nil {
		return nil, err
	}

	healthyRecords := make(map[string][]string)

	serviceRegex := regexp.MustCompile("^localtargets")
	for _, endpoint := range dnsEndpoint.Spec.Endpoints {
		local := serviceRegex.Match([]byte(endpoint.DNSName))
		if !local && endpoint.RecordType == "A" {
			if len(endpoint.Targets) > 0 {
				healthyRecords[endpoint.DNSName] = endpoint.Targets
			}
		}
	}

	return healthyRecords, nil
}

func (r *GslbReconciler) hostsToCSV(gslb *k8gbv1beta1.Gslb) string {
	var hosts []string
	for _, r := range gslb.Spec.Ingress.Rules {
		hosts = append(hosts, r.Host)
	}
	return strings.Join(hosts, ", ")
}
