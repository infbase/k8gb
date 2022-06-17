package controllers

import (
	"context"
	"fmt"
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
	settings := provideSettings(t, predefinedConfig)
	m := dns.NewMockProvider(ctrl)

	m.EXPECT().GslbIngressExposedIPs(gomock.Any()).Return([]string{}, nil).Times(1)
	m.EXPECT().SaveDNSEndpoint(gomock.Any(), gomock.Any()).Return(fmt.Errorf("save DNS error")).Times(1)
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
