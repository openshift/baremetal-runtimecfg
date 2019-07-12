package render

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"text/template"

	"github.com/sirupsen/logrus"

	"github.com/openshift/baremetal-runtimecfg/pkg/config"
)

const ext = ".tmpl"

var extLen = len(ext)

var log = logrus.New()

func renderFile(outDir string, templatePath string, cfg config.Node) error {
	tmpl, err := template.ParseFiles(templatePath)
	if err != nil {
		log.WithFields(logrus.Fields{
			"path": templatePath,
		}).Error("Failed to parse template")
		return err
	}

	baseName := path.Base(templatePath)
	outPath := path.Join(outDir, baseName[:len(baseName)-extLen])
	outFile, err := os.Create(outPath)
	if err != nil {
		log.WithFields(logrus.Fields{
			"path": outPath,
		}).Error("Failed to create file")
		return err
	}
	defer outFile.Close()

	log.WithFields(logrus.Fields{
		"path": outPath,
	}).Info("Runtimecfg rendering template")
	return tmpl.Execute(outFile, cfg)
}

func Render(outDir string, paths []string, cfg config.Node) error {
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

		err := renderFile(outDir, templatePath, cfg)
		if err != nil {
			log.WithFields(logrus.Fields{
				"path": templatePath,
			}).Error("Failed to render template")
		}
	}
	return nil
}
