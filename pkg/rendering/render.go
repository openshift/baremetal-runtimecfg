package rendering

import (
	"os"
	"text/template"
	"github.com/openshift/bm-runtimecfg/pkg/config"
)

func RenderDir(nodeCfg config.Node, pattern string) error {
	if pattern == "" {
		pattern = "*.tmpl"
	}
	t := template.Must(template.New("bm-runtimcfg-tmpl").ParseGlob("*.tmpl"))
	return t.Execute(os.Stdout, config)

