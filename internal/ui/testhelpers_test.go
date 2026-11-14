package ui

import "os"

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}

func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}
