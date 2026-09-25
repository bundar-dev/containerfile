package containerfile

// staticScanner serves an index.html at the root or in public/ with
// unprivileged nginx on port 8080. It is tried last, as a catch-all.
type staticScanner struct{}

func (staticScanner) Name() string { return "static" }

func (staticScanner) Detect(src *Source, forced bool) (*Plan, bool) {
	dir := "."
	switch {
	case src.File("index.html"):
	case src.File("public/index.html"):
		dir = "public"
	case !forced:
		return nil, false
	}
	plan := newPlan("static")
	plan.Port = 8080
	plan.Ignore = []string{"node_modules"}
	plan.Assigns = map[string]any{"dir": dir}
	return plan, true
}

func (staticScanner) Render(p *Plan) (string, error) {
	return NginxStage("", p.Assigns["dir"].(string)) + "\n", nil
}
