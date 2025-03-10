/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta1

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DashboardSourceType string

const (
	DashboardSourceTypeRawJson    DashboardSourceType = "json"
	DashboardSourceTypeGzipJson   DashboardSourceType = "gzipJson"
	DashboardSourceTypeUrl        DashboardSourceType = "url"
	DashboardSourceTypeJsonnet    DashboardSourceType = "jsonnet"
	DashboardSourceTypeGrafanaCom DashboardSourceType = "grafana"
	DefaultResyncPeriod                               = "5m"
)

type GrafanaDashboardDatasource struct {
	InputName      string `json:"inputName"`
	DatasourceName string `json:"datasourceName"`
}

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// GrafanaDashboardSpec defines the desired state of GrafanaDashboard
type GrafanaDashboardSpec struct {
	// dashboard json
	// +optional
	Json string `json:"json,omitempty"`

	// GzipJson the dashboard's JSON compressed with Gzip. Base64-encoded when in YAML.
	// +optional
	GzipJson []byte `json:"gzipJson,omitempty"`

	// dashboard url
	// +optional
	Url string `json:"url,omitempty"`

	// Jsonnet
	// +optional
	Jsonnet string `json:"jsonnet,omitempty"`

	// grafana.com/dashboards
	// +optional
	GrafanaCom *GrafanaComDashboardReference `json:"grafanaCom,omitempty"`

	// selects Grafanas for import
	InstanceSelector *metav1.LabelSelector `json:"instanceSelector"`

	// folder assignment for dashboard
	// +optional
	FolderTitle string `json:"folder,omitempty"`

	// plugins
	// +optional
	Plugins PluginList `json:"plugins,omitempty"`

	// Cache duration for dashboards fetched from URLs
	// +optional
	ContentCacheDuration metav1.Duration `json:"contentCacheDuration,omitempty"`
	// how often the dashboard is refreshed, defaults to 24h if not set
	// +optional
	ResyncPeriod string `json:"resyncPeriod,omitempty"`

	// maps required data sources to existing ones
	// +optional
	Datasources []GrafanaDashboardDatasource `json:"datasources,omitempty"`

	// allow to import this resources from an operator in a different namespace
	// +optional
	AllowCrossNamespaceImport *bool `json:"allowCrossNamespaceImport,omitempty"`
}

// GrafanaComDashbooardReference is a reference to a dashboard on grafana.com/dashboards
type GrafanaComDashboardReference struct {
	Id       int  `json:"id"`
	Revision *int `json:"revision,omitempty"`
}

// GrafanaDashboardStatus defines the observed state of GrafanaDashboard
type GrafanaDashboardStatus struct {
	ContentCache     []byte      `json:"contentCache,omitempty"`
	ContentTimestamp metav1.Time `json:"contentTimestamp,omitempty"`
	ContentUrl       string      `json:"contentUrl,omitempty"`
	Hash             string      `json:"hash,omitempty"`
	// The dashboard instanceSelector can't find matching grafana instances
	NoMatchingInstances bool `json:"NoMatchingInstances,omitempty"`
	// Last time the dashboard was resynced
	LastResync metav1.Time `json:"lastResync,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// GrafanaDashboard is the Schema for the grafanadashboards API
type GrafanaDashboard struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GrafanaDashboardSpec   `json:"spec,omitempty"`
	Status GrafanaDashboardStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// GrafanaDashboardList contains a list of GrafanaDashboard
type GrafanaDashboardList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GrafanaDashboard `json:"items"`
}

func (in *GrafanaDashboard) Hash(dashboardJson []byte) string {
	hash := sha256.New()
	hash.Write(dashboardJson)
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func (in *GrafanaDashboard) Unchanged(hash string) bool {
	return in.Status.Hash == hash
}

func (in *GrafanaDashboard) ResyncPeriodHasElapsed() bool {
	deadline := in.Status.LastResync.Add(in.GetResyncPeriod())
	return time.Now().After(deadline)
}

func (in *GrafanaDashboard) GetResyncPeriod() time.Duration {
	if in.Spec.ResyncPeriod == "" {
		in.Spec.ResyncPeriod = DefaultResyncPeriod
		return in.GetResyncPeriod()
	}

	duration, err := time.ParseDuration(in.Spec.ResyncPeriod)
	if err != nil {
		in.Spec.ResyncPeriod = DefaultResyncPeriod
		return in.GetResyncPeriod()
	}

	return duration
}

func (in *GrafanaDashboard) GetSourceTypes() []DashboardSourceType {
	var sourceTypes []DashboardSourceType

	if in.Spec.Json != "" {
		sourceTypes = append(sourceTypes, DashboardSourceTypeRawJson)
	}

	if in.Spec.GzipJson != nil {
		sourceTypes = append(sourceTypes, DashboardSourceTypeGzipJson)
	}

	if in.Spec.Url != "" {
		sourceTypes = append(sourceTypes, DashboardSourceTypeUrl)
	}

	if in.Spec.Jsonnet != "" {
		sourceTypes = append(sourceTypes, DashboardSourceTypeJsonnet)
	}

	if in.Spec.GrafanaCom != nil {
		sourceTypes = append(sourceTypes, DashboardSourceTypeGrafanaCom)
	}

	return sourceTypes
}

func (in *GrafanaDashboard) GetContentCache() []byte {
	return in.Status.getContentCache(in.Spec.Url, in.Spec.ContentCacheDuration.Duration)
}

// getContentCache returns content cache when the following conditions are met: url is the same, data is not expired, gzipped data is not corrupted
func (in *GrafanaDashboardStatus) getContentCache(url string, cacheDuration time.Duration) []byte {
	if in.ContentUrl != url {
		return []byte{}
	}

	notExpired := cacheDuration <= 0 || in.ContentTimestamp.Add(cacheDuration).After(time.Now())
	if !notExpired {
		return []byte{}
	}

	cache, err := Gunzip(in.ContentCache)
	if err != nil {
		return []byte{}
	}

	return cache
}

func (in *GrafanaDashboard) IsAllowCrossNamespaceImport() bool {
	if in.Spec.AllowCrossNamespaceImport != nil {
		return *in.Spec.AllowCrossNamespaceImport
	}
	return false
}

func Gunzip(compressed []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, err
	}

	return io.ReadAll(gz)
}

func Gzip(content []byte) ([]byte, error) {
	buf := bytes.NewBuffer([]byte{})
	gz := gzip.NewWriter(buf)

	_, err := gz.Write(content)
	if err != nil {
		return nil, err
	}

	if err := gz.Close(); err != nil {
		return nil, err
	}

	return io.ReadAll(buf)
}

func (in *GrafanaDashboardList) Find(namespace string, name string) *GrafanaDashboard {
	for _, dashboard := range in.Items {
		if dashboard.Namespace == namespace && dashboard.Name == name {
			return &dashboard
		}
	}
	return nil
}

func init() {
	SchemeBuilder.Register(&GrafanaDashboard{}, &GrafanaDashboardList{})
}
