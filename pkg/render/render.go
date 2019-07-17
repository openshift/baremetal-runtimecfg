package render

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"text/template"

	"github.com/Masterminds/sprig"
	"github.com/sirupsen/logrus"
)

const ext = ".tmpl"

var extLen = len(ext)

var log = logrus.New()

func RenderFile(renderPath, templatePath string, cfg interface{}) error {
	funcs := sprig.TxtFuncMap()
	tmpl, err := template.New(templatePath).Funcs(funcs).ParseFiles(templatePath)
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
	tempPaths := paths
	if len(paths) == 1 {
		fi, err := os.Stat(paths[0])
		if err != nil {
			log.WithFields(logrus.Fields{
				"path": paths[0],
			}).Error("Failed to stat file")
		}
		if fi.Mode().IsDir() {
			templateDir := paths[0]
			files, err := ioutil.ReadDir(templateDir)
			if err != nil {
				log.WithFields(logrus.Fields{
					"path": templateDir,
				}).Error("Failed to read template directory")
				return err
			}
			tempPaths = make([]string, 0)
			for _, entryFi := range files {
				if entryFi.Mode().IsRegular() {
					if path.Ext(entryFi.Name()) == ext {
						tempPaths = append(tempPaths, path.Join(templateDir, entryFi.Name()))
					}
				}
			}
		}
	}
	for _, templatePath := range tempPaths {
		if path.Ext(templatePath) != ext {
			return fmt.Errorf("Template %s does not have the right extension. Must be '%s'", templatePath, ext)
		}

		baseName := path.Base(templatePath)
		renderPath := path.Join(outDir, baseName[:len(baseName)-extLen])
		err := RenderFile(renderPath, templatePath, cfg)
		if err != nil {
			log.WithFields(logrus.Fields{
				"path": templatePath,
				"err":  err,
			}).Error("Failed to render template")
		}
	}
	return nil
}
