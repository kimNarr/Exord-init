package exordinit

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"text/template"
)

//go:embed skill/exord-init/assets/templates/*/*.tmpl
var templateFiles embed.FS

type TemplateData struct{ ProjectSummary string }

func RenderTemplate(language, name string, data TemplateData) (string, error) {
	if language != "en" && language != "ko" {
		return "", errors.New("unsupported template language")
	}
	path := fmt.Sprintf("skill/exord-init/assets/templates/%s/%s.tmpl", language, name)
	source, err := templateFiles.ReadFile(path)
	if err != nil {
		return "", err
	}
	tmpl, err := template.New(name).Option("missingkey=error").Parse(string(source))
	if err != nil {
		return "", err
	}
	var output bytes.Buffer
	if err := tmpl.Execute(&output, data); err != nil {
		return "", err
	}
	return output.String(), nil
}
