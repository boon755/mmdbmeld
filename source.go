package mmdbmeld

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"

	"github.com/maxmind/mmdbwriter/mmdbtype"
)

// Source describes a generic geoip data source.
type Source interface {
	Name() string
	NextEntry() (*SourceEntry, error)
	Err() error
}

// SourceEntry describes a geoip data source entry.
// Either Net or both From and To must be set.
type SourceEntry struct {
	Net    *net.IPNet
	From   net.IP
	To     net.IP
	Values map[string]SourceValue
}

// SourceValue holds an unprocessed source data value, including its type.
type SourceValue struct {
	Type  string
	Value string
}

// LoadSources loads the given input files from the database config.
func LoadSources(dbConfig DatabaseConfig) ([]Source, error) {
	sources := make([]Source, 0, len(dbConfig.Inputs))
	for _, input := range dbConfig.Inputs {
		switch {
		case strings.HasSuffix(input.File, ".csv"):
			s, err := LoadCSVSource(input, dbConfig.Types)
			if err != nil {
				return nil, fmt.Errorf("failed to load input file %s: %w", input.File, err)
			}
			sources = append(sources, s)
		case strings.HasSuffix(input.File, ".ipfire.txt"):
			s, err := LoadIPFireSource(input, dbConfig.Types)
			if err != nil {
				return nil, fmt.Errorf("failed to load input file %s: %w", input.File, err)
			}
			sources = append(sources, s)
		default:
			return nil, fmt.Errorf("unsupported input file: %s", input.File)
		}
	}

	return sources, nil
}

// ToMMDBMap transforms the source entry to a mmdb map type.
func (se SourceEntry) ToMMDBMap(optim Optimizations) (mmdbtype.Map, error) {
	m := mmdbtype.Map{}
	for key, entry := range se.Values {
		// Transform value to mmdb type.
		mmdbVal, err := entry.ToMMDBType(optim)
		if err != nil {
			return nil, fmt.Errorf("failed to transform %s with value %s (of type %s): %w", key, entry.Value, entry.Type, err)
		}

		// Get sub map for entry.
		keyParts := strings.Split(key, ".")
		mapForEntry := m
		for i := 0; i < len(keyParts)-1; i++ {
			subMapVal, ok := mapForEntry[mmdbtype.String(keyParts[i])]
			if !ok {
				nextMapForEntry := mmdbtype.Map{}
				mapForEntry[mmdbtype.String(keyParts[i])] = nextMapForEntry
				mapForEntry = nextMapForEntry
			} else {
				mapForEntry, ok = subMapVal.(mmdbtype.Map)
				if !ok {
					return nil, fmt.Errorf("failed to transform %s: submap %s already exists but is a %T, and not a map", key, strings.Join(keyParts[:1], "."), subMapVal)
				}
			}
		}

		// Set value in (sub) map.
		mapForEntry[mmdbtype.String(keyParts[len(keyParts)-1])] = mmdbVal
	}

	return m, nil
}

// ToMMDBType transforms the source value to the correct mmdb type.
func (sv SourceValue) ToMMDBType(optim Optimizations) (mmdbtype.DataType, error) {
	subType, isArrayType := strings.CutPrefix(sv.Type, "array:")
	if isArrayType {
		return toMMDBArray(subType, sv.Value, optim)
	}

	return toMMDBType(sv.Type, sv.Value, optim)
}

func toMMDBType(fieldType, fieldValue string, optim Optimizations) (mmdbtype.DataType, error) {
	switch fieldType {
	case "bool":
		v, err := strconv.ParseBool(fieldValue)
		if err != nil {
			return nil, err
		}
		return mmdbtype.Bool(v), nil

	case "string":
		return mmdbtype.String(fieldValue), nil

	case "hexbytes":
		v, err := hex.DecodeString(fieldValue)
		if err != nil {
			return nil, err
		}
		return mmdbtype.Bytes(v), nil

	case "int32":
		v, err := strconv.ParseInt(fieldValue, 10, 32)
		if err != nil {
			return nil, err
		}
		return mmdbtype.Int32(int32(v)), nil

	case "uint16":
		v, err := strconv.ParseUint(fieldValue, 10, 16)
		if err != nil {
			return nil, err
		}
		return mmdbtype.Uint16(uint16(v)), nil

	case "uint32":
		v, err := strconv.ParseUint(fieldValue, 10, 32)
		if err != nil {
			return nil, err
		}
		return mmdbtype.Uint32(uint32(v)), nil

	case "uint64":
		v, err := strconv.ParseUint(fieldValue, 10, 64)
		if err != nil {
			return nil, err
		}
		return mmdbtype.Uint64(v), nil

	case "float32":
		v, err := strconv.ParseFloat(fieldValue, 32)
		if err != nil {
			return nil, err
		}
		if optim.FloatDecimals != 0 {
			v = roundToDecimalPlaces(v, optim.FloatDecimals)
		}
		return mmdbtype.Float32(v), nil

	case "float64":
		v, err := strconv.ParseFloat(fieldValue, 64)
		if err != nil {
			return nil, err
		}
		if optim.FloatDecimals != 0 {
			v = roundToDecimalPlaces(v, optim.FloatDecimals)
		}
		return mmdbtype.Float64(v), nil

	default:
		return nil, errors.New("unsupport type")
	}
}

func toMMDBArray(fieldType, fieldValue string, optim Optimizations) (mmdbtype.DataType, error) {
	fields := strings.Fields(fieldValue)
	array := make([]mmdbtype.DataType, 0, len(fields))

	for i, field := range strings.Fields(fieldValue) {
		entry, err := toMMDBType(fieldType, field, optim)
		if err != nil {
			return nil, fmt.Errorf("array entry #%d is invalid: %w", i, err)
		}
		array = append(array, entry)
	}

	return mmdbtype.Slice(array), nil
}

func roundToDecimalPlaces(num float64, decimalPlaces int) float64 {
	if decimalPlaces < 0 {
		decimalPlaces = 0
	}
	shift := math.Pow(10, float64(decimalPlaces))
	return math.Round(num*shift) / shift
}
