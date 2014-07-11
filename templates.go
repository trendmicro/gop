package gop

import (
	"html/template"
)

func (g *Req) Render(templateData interface{}, templates ...string) error {
	templateDir, _ := g.Cfg.GetPath("gop", "template_dir", "./templates")

	templateFilenames := make([]string, len(templates))
	for i := range templates {
		templateFilenames[i] = templateDir + "/" + templates[i] + ".ght"
	}
	tmpl, err := template.ParseFiles(templateFilenames...)
	if err != nil {
		return err
	}

	g.W.Header().Set("Content-Type", "text/html")
	err = tmpl.Execute(g.W, templateData)
	if err != nil {
		return ServerError("Failed to execute template: " + err.Error())
	}
	return nil
}
