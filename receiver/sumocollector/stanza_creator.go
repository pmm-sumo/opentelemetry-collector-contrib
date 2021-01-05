package sumocollector

import (
	"context"
	"fmt"
	stanza "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/stanzareceiver"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configmodels"
	"go.opentelemetry.io/collector/consumer"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

type FileOperator struct {
	OperatorID   string   `json:"id" yaml:"id"`
	OperatorType string   `json:"type" yaml:"type"`
	OperatorInclude        []string `json:"include" yaml:"include"`
}

func (fo *FileOperator) ID() string        { return fo.OperatorID }
func (fo *FileOperator) Type() string      { return fo.OperatorType }
func (fo *FileOperator) Include() []string { return fo.OperatorInclude }

func buildLogsReceiver(logger *zap.Logger, path string, consumer consumer.LogsConsumer) (component.LogsReceiver, error) {
	pipelineYaml := fmt.Sprintf(`
- type: file_input
  include:
    - %s
  start_at: beginning`,
		path)

	pipelineCfg := stanza.OperatorConfig{}
	err := yaml.Unmarshal([]byte(pipelineYaml), &pipelineCfg)
	if err != nil {
		logger.Error("Failed to unmarshal config pipeline", zap.Error(err))
		return nil, err
	}

	defaultConfig := stanza.NewFactory().CreateDefaultConfig()

	stanzaConfig := stanza.Config{
		ReceiverSettings: configmodels.ReceiverSettings{
			TypeVal: defaultConfig.Type(),
			NameVal: defaultConfig.Name(),
		},
		OffsetsFile:      "",
		PluginDir:        "",
		Operators:        pipelineCfg,
	}

	return stanza.NewFactory().CreateLogsReceiver(context.Background(), component.ReceiverCreateParams{
		Logger:               logger,
	}, &stanzaConfig, consumer)
}
