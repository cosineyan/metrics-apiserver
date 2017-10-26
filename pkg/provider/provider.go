package provider

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	apierr "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/pkg/api"
	"k8s.io/metrics/pkg/apis/custom_metrics"

	"github.com/kubernetes-incubator/custom-metrics-apiserver/pkg/provider"
)

type memoryProvider struct {
	client dynamic.ClientPool
	mapper apimeta.RESTMapper

	values map[provider.MetricInfo]int64
}

func NewMemoryProvider(client dynamic.ClientPool, mapper apimeta.RESTMapper) provider.CustomMetricsProvider {
	return &memoryProvider{
		client: client,
		mapper: mapper,
		values: make(map[provider.MetricInfo]int64),
	}
}

func (m *memoryProvider) valueFor(groupResource schema.GroupResource, metricName string, namespaced bool) (int64, error) {
	info := provider.MetricInfo{
		GroupResource: groupResource,
		Metric:        metricName,
		Namespaced:    namespaced,
	}

	info, _, err := info.Normalized(m.mapper)
	if err != nil {
		return 0, err
	}

	value := m.values[info]
	value += 1
	m.values[info] = value

	return value, nil
}

func (m *memoryProvider) metricFor(value int64, groupResource schema.GroupResource, namespace string, name string, metricName string) (*custom_metrics.MetricValue, error) {
	kind, err := m.mapper.KindFor(groupResource.WithVersion(""))
	if err != nil {
		return nil, err
	}

	return &custom_metrics.MetricValue{
		DescribedObject: api.ObjectReference{
			APIVersion: groupResource.Group + "/" + runtime.APIVersionInternal,
			Kind:       kind.Kind,
			Name:       name,
			Namespace:  namespace,
		},
		MetricName: metricName,
		Timestamp:  metav1.Time{time.Now()},
		Value:      *resource.NewMilliQuantity(value*100, resource.DecimalSI),
	}, nil
}

func (p *memoryProvider) metricsFor(totalValue int64, groupResource schema.GroupResource, metricName string, list runtime.Object) (*custom_metrics.MetricValueList, error) {
	if !apimeta.IsListType(list) {
		return nil, fmt.Errorf("returned object was not a list")
	}

	res := make([]custom_metrics.MetricValue, 0)

	err := apimeta.EachListItem(list, func(item runtime.Object) error {
		objMeta := item.(metav1.Object)
		value, err := p.metricFor(0, groupResource, objMeta.GetNamespace(), objMeta.GetName(), metricName)
		if err != nil {
			return err
		}
		res = append(res, *value)

		return nil
	})
	if err != nil {
		return nil, err
	}

	for i := range res {
		res[i].Value = *resource.NewMilliQuantity(100*totalValue/int64(len(res)), resource.DecimalSI)
	}

	//return p.metricFor(value, groupResource, "", name, metricName)
	return &custom_metrics.MetricValueList{
		Items: res,
	}, nil
}

func (p *memoryProvider) GetRootScopedMetricByName(groupResource schema.GroupResource, name string, metricName string) (*custom_metrics.MetricValue, error) {
	value, err := p.valueFor(groupResource, metricName, false)
	if err != nil {
		return nil, err
	}
	return p.metricFor(value, groupResource, "", name, metricName)
}

func (p *memoryProvider) GetRootScopedMetricBySelector(groupResource schema.GroupResource, selector labels.Selector, metricName string) (*custom_metrics.MetricValueList, error) {
	// construct a client to list the names of objects matching the label selector
	client, err := p.client.ClientForGroupVersionResource(groupResource.WithVersion(""))
	if err != nil {
		glog.Errorf("unable to construct dynamic client to list matching resource names: %v", err)
		// don't leak implementation details to the user
		return nil, apierr.NewInternalError(fmt.Errorf("unable to list matching resources"))
	}

	totalValue, err := p.valueFor(groupResource, metricName, false)
	if err != nil {
		return nil, err
	}

	// we can construct a this APIResource ourself, since the dynamic client only uses Name and Namespaced
	apiRes := &metav1.APIResource{
		Name:       groupResource.Resource,
		Namespaced: false,
	}

	matchingObjectsRaw, err := client.Resource(apiRes, "").
		List(metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, err
	}
	return p.metricsFor(totalValue, groupResource, metricName, matchingObjectsRaw)
}

func (p *memoryProvider) GetNamespacedMetricByName(groupResource schema.GroupResource, namespace string, name string, metricName string) (*custom_metrics.MetricValue, error) {
	value, err := p.valueFor(groupResource, metricName, true)
	if err != nil {
		return nil, err
	}
	return p.metricFor(value, groupResource, namespace, name, metricName)
}

func (p *memoryProvider) GetNamespacedMetricBySelector(groupResource schema.GroupResource, namespace string, selector labels.Selector, metricName string) (*custom_metrics.MetricValueList, error) {
	// construct a client to list the names of objects matching the label selector
	client, err := p.client.ClientForGroupVersionResource(groupResource.WithVersion(""))
	if err != nil {
		glog.Errorf("unable to construct dynamic client to list matching resource names: %v", err)
		// don't leak implementation details to the user
		return nil, apierr.NewInternalError(fmt.Errorf("unable to list matching resources"))
	}

	totalValue, err := p.valueFor(groupResource, metricName, true)
	if err != nil {
		return nil, err
	}

	// we can construct a this APIResource ourself, since the dynamic client only uses Name and Namespaced
	apiRes := &metav1.APIResource{
		Name:       groupResource.Resource,
		Namespaced: true,
	}

	matchingObjectsRaw, err := client.Resource(apiRes, namespace).
		List(metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, err
	}
	return p.metricsFor(totalValue, groupResource, metricName, matchingObjectsRaw)
}

func (p *memoryProvider) ListAllMetrics() []provider.MetricInfo {
	// TODO: maybe dynamically generate this?
	return []provider.MetricInfo{
		{
			GroupResource: schema.GroupResource{Group: "", Resource: "pods"},
			Metric:        "packets-per-second",
			Namespaced:    true,
		},
		{
			GroupResource: schema.GroupResource{Group: "", Resource: "services"},
			Metric:        "connections-per-second",
			Namespaced:    true,
		},
		{
			GroupResource: schema.GroupResource{Group: "", Resource: "namespaces"},
			Metric:        "queue-length",
			Namespaced:    false,
		},
	}
}
