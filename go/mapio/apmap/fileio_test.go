package apmap

import (
	"os"
	"testing"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/mapfile"
)

// The library opens no file and reads no environment, so everything a test needs from disk is here,
// spelled the way consumers' adapters spell it.

func ReadText(path string) (*Object, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Decode(string(data), path)
}

func WriteText(document Value, path string) (string, error) {
	text, err := Encode(document)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return "", err
	}
	return text, nil
}

func ReadDocument(path string, validate bool, opts ValidateOptions) (*Object, error) {
	document, err := ReadText(path)
	if err != nil {
		return nil, err
	}
	if validate {
		if opts.Source == "" {
			opts.Source = path
		}
		if err := ValidateDocument(document, opts); err != nil {
			return nil, err
		}
	}
	return document, nil
}

func WriteDocument(document Value, path string, validate bool, opts ValidateOptions) (string, error) {
	if validate {
		if opts.Source == "" {
			opts.Source = path
		}
		if err := ValidateDocument(document, opts); err != nil {
			return "", err
		}
	}
	return WriteText(document, path)
}

func InspectFile(path string, options InspectOptions) (Summary, error) {
	document, err := ReadText(path)
	if err != nil {
		return Summary{}, err
	}
	name := options.Name
	if name == "" {
		name = path
	}
	return InspectWith(document, name, options)
}

func ImportMapFile(path string, opts ConvertOptions) (*Object, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if opts.Source == "" {
		opts.Source = path
	}
	return ImportMapText(string(data), opts)
}

func DecodeJSONFile(path string) (Value, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodeJSON(data)
}

func parseMapFile(path string) (*mapfile.MapFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return mapfile.ParseNamed(string(data), path)
}

// LoadContract parses the directory's bundle and makes it the process default.
func LoadContract(directory string) (*Contract, error) {
	contract, err := ParseContract(os.DirFS(directory), directory)
	if err != nil {
		return nil, err
	}
	contract.Source = "directory " + directory
	SetContract(contract)
	return contract, nil
}

// LoadSchema is the current schema for "" and the named file otherwise.
func LoadSchema(path string) (*Object, error) {
	if path == "" {
		return CurrentSchema()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &SchemaNotFoundError{Message: "cannot read the APMap schema at " + path, Cause: err}
	}
	return ParseSchema(path, data)
}

func requireSchema(t *testing.T) {
	t.Helper()
	if _, err := CurrentSchema(); err != nil {
		t.Fatalf("no APMap contract is loaded: %v", err)
	}
}
