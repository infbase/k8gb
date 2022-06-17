package controllers

import (
	"context"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	externaldns "sigs.k8s.io/external-dns/endpoint"
	"testing"

	k8gbv1beta1 "github.com/k8gb-io/k8gb/api/v1beta1"

	"github.com/golang/mock/gomock"
	"github.com/k8gb-io/k8gb/controllers/providers/assistant"
	"github.com/k8gb-io/k8gb/controllers/providers/dns"
	"github.com/stretchr/testify/require"
)



func TestStrategyWeight(t *testing.T) {
	// arrange
	targetsEU := assistant.NewTargets()
	targetsUS := assistant.NewTargets()
	targetsZA := assistant.NewTargets()
	targetsUS["us"] = assistant.Target{IPs: []string{"10.0.0.1", "10.0.0.2"}}
	targetsEU["eu"] = assistant.Target{IPs: []string{"10.10.0.1", "10.10.0.2"}}
	targetsZA["za"] = assistant.Target{IPs: []string{"10.22.0.1", "10.22.0.2", "10.22.1.1"}}
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	crSampleYaml = "../deploy/crds/k8gb.absa.oss_v1beta1_gslb_cr_weight.yaml"
	settings := provideSettings(t, predefinedConfig)
	m := dns.NewMockProvider(ctrl)

	var expectedDNSEndpoint = &externaldns.DNSEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("k8gb-ns-%s", "externalDNSTypeCommon"),
			Namespace:   settings.config.K8gbNamespace,
			Annotations: map[string]string{"k8gb.absa.oss/dnstype": "local",
				"k8gb.absa.oss/weight-round-robin":
				"[{\"region\":\"us\",\"weight\":50,\"targets\":[\"10.0.0.1\",\"10.0.0.2\"]}," +
				"{\"region\":\"za\",\"weight\":15,\"targets\":[\"10.22.0.1\",\"10.22.0.2\",\"10.22.1.1\"]}," +
				"{\"region\":\"eu\",\"weight\":35,\"targets\":[\"10.10.0.1\",\"10.10.0.2\"]}]"},
		},
		Spec: externaldns.DNSEndpointSpec{
			Endpoints: []*externaldns.Endpoint{
				{
					DNSName:    settings.config.DNSZone,
					RecordTTL:  30,
					RecordType: "NS",
					//Targets:    settings.gslb. a.TargetNSNamesSorted,
				},
				{
					DNSName:    "gslb-ns-us-cloud.example.com",
					RecordTTL:  30,
					RecordType: "A",
					Targets:    []string{},
				},
			},
		},
	}


	m.EXPECT().GslbIngressExposedIPs(gomock.Any()).Return([]string{}, nil).Times(1)
	m.EXPECT().SaveDNSEndpoint(gomock.Any(), gomock.Any()).Do().Return(fmt.Errorf("save DNS error")).Times(1)
	m.EXPECT().CreateZoneDelegationForExternalDNS(gomock.Any()).Return(nil).AnyTimes()

	m.EXPECT().GetExternalTargets(gomock.Any()).Return(targetsEU).Times(1)
	m.EXPECT().GetExternalTargets(gomock.Any()).Return(targetsUS).Times(1)
	m.EXPECT().GetExternalTargets(gomock.Any()).Return(targetsZA).Times(1)
	settings.reconciler.DNSProvider = m
	settings.gslb.Spec.Strategy.Weight = make(map[string]k8gbv1beta1.Percentage, 0)
	settings.gslb.Spec.Strategy.Weight["eu"] = "15%"
	settings.gslb.Spec.Strategy.Weight["us"] = "50%"
	settings.gslb.Spec.Strategy.Weight["za"] = "25%"

	//settings.reconciler.DepResolver.ResolveGslbSpec()
	// act
	_, err := settings.reconciler.Reconcile(context.TODO(), settings.request)
	require.Error(t, err)
	// assert

}
