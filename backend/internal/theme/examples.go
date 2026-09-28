package theme

import "strings"

// Example is a theme anybody may start from. The editor offers them beside
// a blank sheet, and the demo organization gets them installed.
type Example struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Spec        Spec   `json:"spec"`
}

// Examples are the themes shipped with the product; docs/design/newdesign.jpeg
// is the picture Deep-Tech is drawn from.
func Examples() []Example {
	examples := []Example{deepTech(), constellation()}
	for i := range examples {
		examples[i].Spec.normalise()
	}
	return examples
}

// ExampleByKey finds one example, or nil.
func ExampleByKey(key string) *Example {
	for _, e := range Examples() {
		if e.Key == key {
			example := e
			return &example
		}
	}
	return nil
}

func intPtr(n int) *int { return &n }

// svgDataURI makes a small drawing safe to carry inside a stylesheet: the
// characters CSS or the validator would trip on are percent encoded, so
// no tag ever appears in the text.
func svgDataURI(svg string) string {
	encoded := strings.NewReplacer("\n", "", "<", "%3C", ">", "%3E", "#", "%23", "\"", "%22", " ", "%20").Replace(strings.TrimSpace(svg))
	return "data:image/svg+xml," + encoded
}

// constellation is Deep-Tech with the picture's other half: the lattice of
// points and lines behind the content, drawn as a tile so the theme still
// carries no file, and a deeper teal in the dark.
func constellation() Example {
	base := deepTech()
	light := map[string]string{}
	for k, v := range base.Spec.Colors.Light {
		light[k] = v
	}
	dark := map[string]string{}
	for k, v := range base.Spec.Colors.Dark {
		dark[k] = v
	}
	for k, v := range map[string]string{
		"backdrop-from": "#eef3f8", "backdrop-to": "#cfdbe8", "accent": "#1f7fb8", "accent-hover": "#186a9a",
		"accent-subtle": "#dcedf8", "focus": "#1f7fb8", "selection": "#d3e6f4", "chart-1": "#1f7fb8",
	} {
		light[k] = v
	}
	for k, v := range map[string]string{
		"canvas": "#0a141d", "surface": "#10202c", "surface-raised": "#172b3a", "surface-sunken": "#071017",
		"surface-overlay": "#14283a", "surface-glass": "rgb(16 32 44 / 0.6)", "backdrop-from": "#123141",
		"backdrop-to": "#07111a", "border": "#1f3a4c", "border-strong": "#2f5468",
		"accent": "#4fd1c5", "accent-hover": "#7de3d9", "accent-subtle": "#123a3d", "primary": "#4fd1c5",
		"primary-hover": "#7de3d9", "focus": "#4fd1c5", "selection": "#174549", "chart-1": "#4fd1c5", "chart-5": "#5cc8ff",
	} {
		dark[k] = v
	}
	// Two lattices, one near and one far, each a tile whose lines stay clear
	// of its edges so the repeat never shows a seam. They drift slowly in
	// different directions and the glow breathes under them, which is what
	// gives the backdrop its depth; anyone who asked for less motion gets the
	// same picture standing still.
	near := svgDataURI(`<svg xmlns="http://www.w3.org/2000/svg" width="520" height="520" viewBox="0 0 520 520" fill="none" stroke="rgb(120 165 205 / 0.42)" stroke-width="1">
<path d="M70 90L180 50L260 120L370 70L450 140M180 50L150 200M260 120L150 200M150 200L90 330L200 400L300 330L260 120M300 330L370 70M300 330L430 300L450 140M200 400L230 470L360 450L430 300M90 330L70 90"/>
<g fill="rgb(140 185 225 / 0.6)" stroke="none"><circle cx="70" cy="90" r="2.6"/><circle cx="180" cy="50" r="2"/><circle cx="260" cy="120" r="3.2"/><circle cx="370" cy="70" r="2"/><circle cx="450" cy="140" r="2.4"/><circle cx="150" cy="200" r="2.8"/><circle cx="90" cy="330" r="2"/><circle cx="200" cy="400" r="3"/><circle cx="300" cy="330" r="2.4"/><circle cx="430" cy="300" r="2"/><circle cx="230" cy="470" r="2"/><circle cx="360" cy="450" r="2.6"/></g>
</svg>`)
	far := svgDataURI(`<svg xmlns="http://www.w3.org/2000/svg" width="760" height="760" viewBox="0 0 760 760" fill="none" stroke="rgb(120 165 205 / 0.24)" stroke-width="1">
<path d="M120 140L300 80L470 160L640 100M300 80L250 300M470 160L250 300M250 300L140 470L330 560L500 430L470 160M500 430L640 100M500 430L660 520L640 660M330 560L360 680L520 640L660 520M140 470L120 140"/>
<g fill="rgb(140 185 225 / 0.35)" stroke="none"><circle cx="120" cy="140" r="3.5"/><circle cx="300" cy="80" r="2.5"/><circle cx="470" cy="160" r="4"/><circle cx="640" cy="100" r="2.5"/><circle cx="250" cy="300" r="3.5"/><circle cx="140" cy="470" r="2.5"/><circle cx="330" cy="560" r="4"/><circle cx="500" cy="430" r="3"/><circle cx="660" cy="520" r="2.5"/><circle cx="360" cy="680" r="2.5"/><circle cx="520" cy="640" r="3"/><circle cx="640" cy="660" r="2.5"/></g>
</svg>`)
	css := `.bg-backdrop {
  background-color: var(--color-backdrop-to);
  background-image:
    url("` + near + `"),
    url("` + far + `"),
    radial-gradient(circle at 15% 10%, var(--color-backdrop-from) 0, transparent 48%),
    radial-gradient(circle at 85% 90%, var(--color-accent-subtle) 0, transparent 42%);
  background-size: 520px 520px, 760px 760px, 200% 200%, 200% 200%;
  background-position: 0 0, 0 0, 0% 0%, 100% 100%;
  background-attachment: fixed;
  animation: constellation-drift 240s linear infinite;
}

@keyframes constellation-drift {
  0%   { background-position: 0px 0px, 0px 0px, 0% 0%, 100% 100%; }
  50%  { background-position: 520px 260px, -380px 190px, 30% 20%, 70% 80%; }
  100% { background-position: 1040px 520px, -760px 380px, 0% 0%, 100% 100%; }
}

@media (prefers-reduced-motion: reduce) {
  .bg-backdrop { animation: none; }
}

/* The picture's column: the rail and the sidebar in dark slate whatever the
   palette, done by giving that column its own values for the tokens the
   kit paints it with. */
[data-rail], [data-sidebar] {
  --color-surface: #1b2a3a;
  --color-surface-raised: #26394d;
  --color-surface-overlay: #223548;
  --color-border: #2c4157;
  --color-border-strong: #3d5670;
  --color-ink: #e6eef8;
  --color-ink-muted: #a7b8cc;
  --color-ink-subtle: #7b8fa6;
  --color-ink-disabled: #52657a;
  --color-accent: #7fd4ff;
  --color-accent-hover: #a5e1ff;
  --color-accent-subtle: #24405a;
  --color-selection: #24405a;
}`
	shadows := map[string]string{}
	for k, v := range base.Spec.Shadows {
		shadows[k] = v
	}
	return Example{
		Key:         "constellation",
		Name:        "Constellation",
		Description: "Deep-Tech with two lattices of points and lines drifting behind everything, and teal after dark.",
		Spec: Spec{
			Colors:  Palette{Light: light, Dark: dark},
			Shape:   Shape{RadiusControl: intPtr(8), RadiusOverlay: intPtr(12)},
			Shadows: shadows,
			CSS:     css,
		},
	}
}

// deepTech is cool slate and glass: a soft grey-blue gradient in the light,
// a night of navy in the dark, one cyan accent for what is current, cards
// that float on the backdrop, and the status colours kept apart from it.
func deepTech() Example {
	light := map[string]string{
		"canvas":                 "#e9eef4",
		"surface":                "#f7f9fc",
		"surface-raised":         "#e3e9f1",
		"surface-sunken":         "#d9e1ea",
		"surface-overlay":        "#ffffff",
		"surface-glass":          "rgb(247 249 252 / 0.72)",
		"backdrop-from":          "#f2f5f9",
		"backdrop-to":            "#c6d3e1",
		"border":                 "#cfd9e5",
		"border-strong":          "#a9b8c9",
		"ink":                    "#17222f",
		"ink-muted":              "#4f6178",
		"ink-subtle":             "#7b8fa6",
		"ink-disabled":           "#aab8c7",
		"primary":                "#17222f",
		"primary-hover":          "#2a3a4e",
		"on-primary":             "#f7f9fc",
		"accent":                 "#2f6fd6",
		"accent-hover":           "#245cb8",
		"accent-subtle":          "#dfe9fb",
		"on-accent":              "#ffffff",
		"focus":                  "#2f6fd6",
		"selection":              "#d6e3fa",
		"danger":                 "#c9463a",
		"danger-hover":           "#a83a30",
		"danger-subtle":          "#fae4e1",
		"success":                "#1f9d7a",
		"success-subtle":         "#dff3ec",
		"warning":                "#c98a1e",
		"warning-subtle":         "#fbefd6",
		"epic":                   "#6f56c5",
		"status-todo":            "#7b8fa6",
		"status-progress":        "#c98a1e",
		"status-done":            "#1f9d7a",
		"status-todo-subtle":     "#e8edf3",
		"status-progress-subtle": "#fbefd6",
		"status-done-subtle":     "#dff3ec",
		"chart-1":                "#2f6fd6",
		"chart-2":                "#1f9d7a",
		"chart-3":                "#6f56c5",
		"chart-4":                "#c98a1e",
		"chart-5":                "#1aa3b8",
		"chart-6":                "#c9463a",
	}
	dark := map[string]string{
		"canvas":                 "#0d1826",
		"surface":                "#15243a",
		"surface-raised":         "#1c2f4a",
		"surface-sunken":         "#0a1320",
		"surface-overlay":        "#1a2c45",
		"surface-glass":          "rgb(21 36 58 / 0.62)",
		"backdrop-from":          "#16294a",
		"backdrop-to":            "#0a1421",
		"border":                 "#274060",
		"border-strong":          "#3a5a82",
		"ink":                    "#e6eef8",
		"ink-muted":              "#a7b8cc",
		"ink-subtle":             "#7b8fa6",
		"ink-disabled":           "#4f6178",
		"primary":                "#5cc8ff",
		"primary-hover":          "#7fd4ff",
		"on-primary":             "#06192b",
		"accent":                 "#5cc8ff",
		"accent-hover":           "#8ad9ff",
		"accent-subtle":          "#173552",
		"on-accent":              "#06192b",
		"focus":                  "#5cc8ff",
		"selection":              "#1f4266",
		"danger":                 "#f0776b",
		"danger-hover":           "#f59185",
		"danger-subtle":          "#3d2325",
		"success":                "#48d0b0",
		"success-subtle":         "#163a35",
		"warning":                "#f2b85c",
		"warning-subtle":         "#3c2f16",
		"epic":                   "#b39cf0",
		"status-todo":            "#7b8fa6",
		"status-progress":        "#f2b85c",
		"status-done":            "#48d0b0",
		"status-todo-subtle":     "#1c2a3d",
		"status-progress-subtle": "#3c2f16",
		"status-done-subtle":     "#163a35",
		"chart-1":                "#5cc8ff",
		"chart-2":                "#48d0b0",
		"chart-3":                "#b39cf0",
		"chart-4":                "#f2b85c",
		"chart-5":                "#3fb9d6",
		"chart-6":                "#f0776b",
	}
	// The backdrop is drawn, not loaded: a glow in one corner and a faint
	// lattice of points, so the theme carries no file.
	css := `.bg-backdrop {
  background-color: var(--color-backdrop-to);
  background-image:
    radial-gradient(circle at 18% 12%, var(--color-backdrop-from) 0, transparent 46%),
    radial-gradient(circle at 82% 88%, var(--color-accent-subtle) 0, transparent 40%),
    radial-gradient(var(--color-border-strong) 0.8px, transparent 1px);
  background-size: auto, auto, 28px 28px;
  background-attachment: fixed;
}`
	return Example{
		Key:         "deep-tech",
		Name:        "Deep-Tech",
		Description: "Slate and glass: a grey-blue gradient by day, navy by night, one cyan accent.",
		Spec: Spec{
			Colors: Palette{Light: light, Dark: dark},
			Shape:  Shape{RadiusControl: intPtr(8), RadiusOverlay: intPtr(12)},
			Shadows: map[string]string{
				"1": "0 1px 2px rgb(10 20 33 / 0.10)",
				"2": "0 10px 28px rgb(10 20 33 / 0.22)",
				"3": "0 18px 44px rgb(10 20 33 / 0.24), inset 0 1px 0 rgb(255 255 255 / 0.08)",
			},
			CSS: css,
		},
	}
}
