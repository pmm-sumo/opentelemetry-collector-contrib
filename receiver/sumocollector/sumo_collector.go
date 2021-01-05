// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package sumocollector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go.opentelemetry.io/collector/consumer"
	"io"
	"net/http"
	"net/url"
	"strings"

	"go.opentelemetry.io/collector/component"
	"go.uber.org/zap"
)

type sumoCollector struct {
	apiEndpoint  *url.URL
	httpClient   *http.Client
	logger       *zap.Logger
	config       *Config
	nextConsumer consumer.LogsConsumer
	receivers    []component.LogsReceiver
}

var _ component.LogsReceiver = (*sumoCollector)(nil)
var errNilNextConsumer = errors.New("nil nextConsumer")

type Source struct {
	Name           string    `json:"name"`
	ContentType    *string   `json:"contentType,omitempty"`
	Encoding       *string   `json:"encoding,omitempty"`
	Interval       *int      `json:"interval,omitempty"`
	Metrics        *[]string `json:"metrics,omitempty"`
	SourceType     string    `json:"sourceType"`
	PathExpression *string   `json:"pathExpression,omitempty"`
}

type SourcesResponse struct {
	Sources []Source `json:"sources"`
}

// newLogsReceiverCreator creates the receiver_creator with the given parameters.
func newLogsReceiverCreator(params component.ReceiverCreateParams, config *Config, nextConsumer consumer.LogsConsumer) (component.LogsReceiver, error) {
	if nextConsumer == nil {
		return nil, errNilNextConsumer
	}

	if config.API.Endpoint == "" {
		return nil, errors.New("'api.endpoint' config option cannot be empty")
	}

	var apiURL, err = url.Parse(config.API.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("enter a valid URL for 'api.endpoint': %w", err)
	}

	httpClient, err := config.API.ToClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP Client: %w", err)
	}

	h := &sumoCollector{
		config:       config,
		apiEndpoint:  apiURL,
		httpClient:   httpClient,
		logger:       params.Logger,
		nextConsumer: nextConsumer,
	}

	return h, nil
}

func (h *sumoCollector) Start(ctx context.Context, host component.Host) error {
	h.receivers = h.checkSources(ctx)
	for _, receiver := range h.receivers {
		// FIXME: error handling
		_ = receiver.Start(ctx, host)
	}
	return nil
}

func (h *sumoCollector) Shutdown(ctx context.Context) error {
	for _, receiver := range h.receivers {
		// FIXME: error handling
		_ = receiver.Shutdown(ctx)
	}
	return nil
}

func (h *sumoCollector) sourcesURL() string {
	return fmt.Sprintf("%s/api/v1/collectors/%s/sources", h.apiEndpoint, h.config.CollectorID)
}

func (h *sumoCollector) checkSources(ctx context.Context) []component.LogsReceiver {
	url := h.sourcesURL()
	var receivers []component.LogsReceiver

	r, _ := http.NewRequest(http.MethodGet, url, strings.NewReader("")) // URL-encoded payload
	r.SetBasicAuth(h.config.AccessID, h.config.AccessKey)

	response, err := h.httpClient.Do(r)
	if err != nil {
		h.logger.Warn("Failed when doing request", zap.String("url", url), zap.Error(err))
	}

	if response == nil {
		return receivers
	}
	defer response.Body.Close()

	if response.StatusCode != 200 {
		h.logger.Warn("Bad status code", zap.Int("StatusCode", response.StatusCode))
		return receivers
	}

	sources, err := h.handleBody(url, response.Body)
	if err == nil {
		for _, source := range sources.Sources {
			if source.SourceType == "LocalFile" && source.PathExpression != nil {
				lr, err := buildLogsReceiver(h.logger, *source.PathExpression, h.nextConsumer)
				if err == nil {
					receivers = append(receivers, lr)
				}
			}
		}
	}

	return receivers
}

func (h *sumoCollector) handleBody(url string, closer io.ReadCloser) (SourcesResponse, error) {
	var sources SourcesResponse

	err := json.NewDecoder(closer).Decode(&sources)
	if err != nil {
		h.logger.Warn("Error while parsing sources response", zap.String("url", url), zap.Error(err))
		buf := new(strings.Builder)
		io.Copy(buf, closer)
		h.logger.Info(buf.String())
	}

	return sources, err
}
