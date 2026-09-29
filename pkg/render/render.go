package render

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/sirupsen/logrus"
)

const ext = ".tmpl"

var extLen = len(ext)

var log = logrus.New()

func RenderFile(renderPath, templatePath string, cfg interface{}) error {
	funcMap := template.FuncMap{
		"isIPv4": func(addr string) bool {
			ip := net.ParseIP(addr)
			return ip != nil && ip.To4() != nil
		},
		"isIPv6": func(addr string) bool {
			ip := net.ParseIP(addr)
			return ip != nil && ip.To4() == nil
		},
	}
	name := filepath.Base(templatePath)
	tmpl, err := template.New(name).Funcs(funcMap).ParseFiles(templatePath)
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

	// Make sure we propagate any special permissions
	templateStat, err := os.Stat(templatePath)
	if err != nil {
		log.WithFields(logrus.Fields{
			"path": templatePath,
		}).Error("Failed to stat template")
		return err
	}
	err = os.Chmod(renderPath, templateStat.Mode())
	if err != nil {
		log.WithFields(logrus.Fields{
			"path": renderPath,
		}).Error("Failed to set permissions on file")
		return err
	}

	buf := &bytes.Buffer{}
	err = tmpl.Execute(buf, cfg)
	if err != nil {
		log.WithFields(logrus.Fields{
			"path": renderPath,
		}).Error("Failed to render template")
		return err
	}
	// The string we get back is a single line with \n's. For readability,
	// split it and write it line-by-line.
	lines := strings.Split(buf.String(), "\n")
	for _, line := range lines {
		log.Info(line)
	}

	log.WithFields(logrus.Fields{
		"path": renderPath,
	}).Info("Runtimecfg rendering template")
	return tmpl.Execute(renderFile, cfg)
}

// RenderFileAtomic publishes a rendered template without exposing a partial file.
// Unlike RenderFile, it refuses symlink destinations because this writer owns the
// target Corefile rather than the symlink's target.
func RenderFileAtomic(renderPath, templatePath string, cfg interface{}) error {
	contents, mode, err := expand(templatePath, cfg)
	if err != nil {
		return err
	}

	info, err := os.Lstat(renderPath)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink output %q", renderPath)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("output %q is not a regular file", renderPath)
		}
		existing, readErr := os.ReadFile(renderPath)
		if readErr != nil {
			return readErr
		}
		if bytes.Equal(existing, contents) && info.Mode().Perm() == mode.Perm() {
			return nil
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	temporary, err := os.CreateTemp(filepath.Dir(renderPath), ".runtimecfg-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		if err := os.Remove(temporaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.WithField("path", temporaryPath).WithError(err).Warn("Failed to remove temporary file")
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		return errors.Join(err, temporary.Close())
	}
	if _, err := temporary.Write(contents); err != nil {
		return errors.Join(err, temporary.Close())
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, renderPath)
}

func expand(templatePath string, cfg interface{}) ([]byte, os.FileMode, error) {
	funcMap := template.FuncMap{
		"isIPv4": func(addr string) bool {
			ip := net.ParseIP(addr)
			return ip != nil && ip.To4() != nil
		},
		"isIPv6": func(addr string) bool {
			ip := net.ParseIP(addr)
			return ip != nil && ip.To4() == nil
		},
	}
	name := filepath.Base(templatePath)
	tmpl, err := template.New(name).Funcs(funcMap).ParseFiles(templatePath)
	if err != nil {
		return nil, 0, err
	}
	templateStat, err := os.Stat(templatePath)
	if err != nil {
		return nil, 0, err
	}

	buf := &bytes.Buffer{}
	err = tmpl.Execute(buf, cfg)
	if err != nil {
		return nil, 0, err
	}
	return buf.Bytes(), templateStat.Mode(), nil
}

func Render(outDir string, paths []string, cfg interface{}) error {
	tempPaths := paths
	if len(paths) == 1 {
		fi, err := os.Stat(paths[0])
		if err != nil {
			log.WithFields(logrus.Fields{
				"path": paths[0],
			}).Error("Failed to stat file")
			return err
		}
		if fi.Mode().IsDir() {
			templateDir := paths[0]
			files, err := os.ReadDir(templateDir)
			if err != nil {
				log.WithFields(logrus.Fields{
					"path": templateDir,
				}).Error("Failed to read template directory")
				return err
			}
			tempPaths = make([]string, 0)
			for _, entryFi := range files {
				if entryFi.Type().IsRegular() {
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
			return err
		}
	}
	return nil
}
