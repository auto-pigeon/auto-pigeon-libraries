package mapfile

import "os"

// The library opens no file, so reading a fixture is the test's own business.

func ParseFile(path string) (*MapFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseNamed(string(data), path)
}

func WriteFile(m *MapFile, path string) error {
	text, err := Write(m)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(text), 0o644)
}
