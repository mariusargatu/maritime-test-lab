// Package avroserde encodes and decodes Avro records in the Confluent wire
// format (magic byte + 4-byte big-endian schema ID + body), registering each
// schema with the Schema Registry under its TopicNameStrategy subject
// (<topic>-value). It uses hamba/avro for the codec.
//
// Decode keys on the CONSUMER's expected Go type (the reader schema), not on the
// writer's schema ID. That is the correct Confluent consumer behaviour and makes
// decoding immune to the registry assigning the same schema different IDs when
// two services register it concurrently at startup. The writer ID is still
// written into the framing so other Confluent tooling (e.g. Venom's with_avro)
// can resolve it. Runtime producers and consumers share the current schema;
// cross-version resolution is covered by the evolution decode tests.
package avroserde

import (
	"context"
	"encoding/binary"
	"fmt"
	"reflect"

	hambaavro "github.com/hamba/avro/v2"
	"github.com/twmb/franz-go/pkg/sr"
)

const magicByte = 0x00

// Registration ties an event's Go type to its Avro schema and subject.
type Registration struct {
	Subject string // <topic>-value
	Schema  string // the .avsc text
	Example any    // a zero value of the Go type, e.g. avro.VoyageCreated{}
}

type entry struct {
	id    uint32
	codec hambaavro.Schema
}

// Serde encodes/decodes the registered event types.
type Serde struct {
	byType map[reflect.Type]entry
}

// New registers each schema with the registry and returns a ready Serde.
func New(ctx context.Context, registryURL string, regs ...Registration) (*Serde, error) {
	client, err := sr.NewClient(sr.URLs(registryURL))
	if err != nil {
		return nil, fmt.Errorf("avroserde: schema registry client: %w", err)
	}

	byType := make(map[reflect.Type]entry, len(regs))
	for _, r := range regs {
		ss, err := client.CreateSchema(ctx, r.Subject, sr.Schema{Schema: r.Schema, Type: sr.TypeAvro})
		if err != nil {
			return nil, fmt.Errorf("avroserde: register %s: %w", r.Subject, err)
		}
		codec, err := hambaavro.Parse(r.Schema)
		if err != nil {
			return nil, fmt.Errorf("avroserde: parse %s: %w", r.Subject, err)
		}
		byType[reflect.TypeOf(r.Example)] = entry{id: uint32(ss.ID), codec: codec}
	}
	return &Serde{byType: byType}, nil
}

// Encode returns the Confluent-framed bytes for a registered value.
func (s *Serde) Encode(v any) ([]byte, error) {
	e, ok := s.byType[reflect.TypeOf(v)]
	if !ok {
		return nil, fmt.Errorf("avroserde encode: type %T not registered", v)
	}
	body, err := hambaavro.Marshal(e.codec, v)
	if err != nil {
		return nil, fmt.Errorf("avroserde encode: %w", err)
	}
	out := make([]byte, 5, 5+len(body))
	out[0] = magicByte
	binary.BigEndian.PutUint32(out[1:5], e.id)
	return append(out, body...), nil
}

// Decode strips the framing and decodes the body into v (a pointer to a
// registered type) using that type's schema.
func (s *Serde) Decode(b []byte, v any) error {
	if len(b) < 5 || b[0] != magicByte {
		return fmt.Errorf("avroserde decode: not a confluent-framed message")
	}
	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	e, ok := s.byType[t]
	if !ok {
		return fmt.Errorf("avroserde decode: type %s not registered", t)
	}
	if err := hambaavro.Unmarshal(e.codec, b[5:], v); err != nil {
		return fmt.Errorf("avroserde decode: %w", err)
	}
	return nil
}
