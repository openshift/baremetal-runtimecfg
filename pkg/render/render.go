package render

import (
	"errors"
	"os"
	"text/template"

	"github.com/sirupsen/logrus"
)

const ext = ".tmpl"

var extLen = len(ext)

var log = logrus.New()

func RenderFile(renderPath, templatePath string, cfg interface{}) error {
	tmpl, err := template.ParseFiles(templatePath)
	if err != nil {
		log.WithFields(logrus.Fields{
			"path": templatePath,
		}).Error("Failed to parse template")
		return err
	}

	renderFile, err := os.Create(renderPath)
	if err != nil {
		log.WithFields(logrus.Fields{
			"path": renderPath,
		}).Error("Failed to create file")
		return err
	}
	defer renderFile.Close()

	log.WithFields(logrus.Fields{
		"path": renderPath,
	}).Info("Runtimecfg rendering template")
	return tmpl.Execute(renderFile, cfg)
}

func Render(outDir string, paths []string, cfg interface{}) error {
	return errors.New("Test error")
}
