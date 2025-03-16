package internal

import (
	"fmt"
	"os"

	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

// Global topic data
var (
	allTopics       []topicData
	topicNameLookup map[string]topicData
)

// topicData represents a heat pump topic configuration
type topicData struct {
	SensorName     string   `yaml:"sensorName" validate:"required"`
	DecodeFunction string   `yaml:"decodeFunction" validate:"required"`
	EncodeFunction string   `yaml:"encodeFunction"`
	DecodeOffset   int      `yaml:"decodeOffset" validate:"required"`
	DisplayUnit    string   `yaml:"displayUnit"`
	Values         []string `yaml:"values"`
	Min            int      `yaml:"min"`
	Max            int      `yaml:"max"`
	Step           int      `yaml:"step"`
	StateClass     string   `yaml:"stateClass"`
	currentValue   string
}

// LoadTopics loads and parses the topic definition file
func LoadTopics(topicFile string) error {
	log.Info().Str("file", topicFile).Msg("Loading topic definitions")

	// Read the file
	data, err := os.ReadFile(topicFile)
	if err != nil {
		return fmt.Errorf("failed to read topics file: %w", err)
	}

	// Parse the YAML
	err = yaml.Unmarshal(data, &allTopics)
	if err != nil {
		return fmt.Errorf("failed to parse topics file: %w", err)
	}

	// Build the lookup map
	topicNameLookup = make(map[string]topicData, len(allTopics))
	for _, val := range allTopics {
		// Validate topic data
		if val.SensorName == "" {
			log.Warn().Interface("topic", val).Msg("Topic missing sensor name, skipping")
			continue
		}

		// Check for min/max consistency for numeric values
		if val.Min != 0 || val.Max != 0 {
			if val.Min >= val.Max {
				log.Warn().
					Str("sensor", val.SensorName).
					Int("min", val.Min).
					Int("max", val.Max).
					Msg("Topic has invalid min/max range (min >= max), please check configuration")
			}
		}

		topicNameLookup[val.SensorName] = val
	}

	log.Info().
		Int("topicCount", len(allTopics)).
		Msg("Topic definitions loaded successfully")

	return nil
}
