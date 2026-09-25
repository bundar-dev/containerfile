package containerfile

import "testing"

const phoenixMix = `defmodule Hello.MixProject do
  use Mix.Project

  def project do
    [app: :hello, version: "0.1.0", elixir: "~> 1.15", aliases: aliases(), deps: deps()]
  end

  defp deps do
    [{:phoenix, "~> 1.8"}, {:ecto_sql, "~> 3.10"}, {:bandit, "~> 1.5"}]
  end

  defp aliases do
    [
      "assets.setup": ["tailwind.install --if-missing", "esbuild.install --if-missing"],
      "assets.deploy": ["tailwind hello --minify", "esbuild hello --minify", "phx.digest"]
    ]
  end
end
`

func TestElixirPhoenix(t *testing.T) {
	r := generate(t, map[string]string{
		"mix.exs": phoenixMix, "mix.lock": "%{}", ".tool-versions": "elixir 1.18.4-otp-27\n",
		"config/config.exs": "", "config/prod.exs": "", "config/runtime.exs": "",
		"lib/hello.ex": "", "priv/repo/.keep": "", "assets/js/app.js": "",
		"rel/overlays/bin/server": "", "package.json": "{}",
	})
	p := r.Plan
	assertEqual(t, p.Runtime, "elixir")
	assertEqual(t, p.Framework, "phoenix")
	assertEqual(t, p.Port, 4000)
	assertEqual(t, p.Versions["elixir"], "1.18.4")
	assertEqual(t, p.Versions["otp"], "27")
	assertEqual(t, p.Sources["otp"], ".tool-versions")
	assertContains(t, r.Containerfile,
		"ARG ELIXIR_VERSION=1.18.4\nARG OTP_VERSION=27\nARG DEBIAN_VERSION=bookworm",
		"build-essential ca-certificates git",
		"COPY mix.exs mix.lock ./",
		"COPY config/config.exs config/prod.exs config/",
		"RUN mix assets.setup", "COPY assets assets", "RUN mix assets.deploy",
		"COPY config/runtime.exs config/", "COPY rel rel",
		`PHX_SERVER="true"`, "/app/_build/${MIX_ENV}/rel/hello ./",
		`CMD ["/app/bin/server"]`)
	refuteContains(t, r.Containerfile, "npm")
	assertContains(t, r.Ignore, "priv/static/assets")
	if !hasNote(p, "SECRET_KEY_BASE") || !hasNote(p, "Ecto detected") {
		t.Errorf("notes: %q", p.Notes)
	}
}

func TestElixirPhoenixNpm(t *testing.T) {
	r := generate(t, map[string]string{"mix.exs": phoenixMix, "assets/package.json": "{}", "assets/package-lock.json": "{}"})
	assertContains(t, r.Containerfile, "nodejs npm", "RUN npm --prefix assets ci")

	r = generate(t, map[string]string{
		"mix.exs":             "defmodule X.MixProject do\n def project, do: [deps: deps()]\n defp deps, do: [{:phoenix, \"~> 1.7\"}]\nend",
		"assets/package.json": "{}", "native/nif/Cargo.toml": "",
	})
	assertContains(t, r.Containerfile, "RUN npm --prefix assets install", "/rel/app ./")
	if !hasNote(r.Plan, "native/") || !hasNote(r.Plan, "assets.deploy") {
		t.Errorf("notes: %q", r.Plan.Notes)
	}
}

func TestElixirPlain(t *testing.T) {
	r := generate(t, map[string]string{
		"mix.exs":        "defmodule W.MixProject do\n def project, do: [app: :worker, deps: []]\nend",
		".tool-versions": "elixir 1.19.1\nerlang 28.1\n", "lib/worker.ex": "",
	})
	assertEqual(t, r.Plan.Framework, "")
	assertEqual(t, r.Plan.Port, 0)
	assertContains(t, r.Containerfile, "ARG OTP_VERSION=28", "ARG DEBIAN_VERSION=trixie", "COPY mix.exs ./\n",
		`CMD ["/app/bin/worker", "start"]`)
	refuteContains(t, r.Containerfile, "assets", "PHX_SERVER", "EXPOSE")

	bandit := generate(t, map[string]string{"mix.exs": `[app: :api, deps: [{:bandit, "~> 1.0"}]]`})
	assertEqual(t, bandit.Plan.Port, 4000)
	assertEqual(t, bandit.Plan.Env["PHX_SERVER"], "")
}

func TestElixirVersions(t *testing.T) {
	r := generate(t, map[string]string{"mix.exs": "[app: :x]", ".elixir-version": "1.17.3"})
	assertEqual(t, r.Plan.Versions["elixir"], "1.17.3")
	assertEqual(t, r.Plan.Versions["otp"], "27")

	r = generate(t, map[string]string{"mix.exs": "[app: :x]"})
	assertEqual(t, r.Plan.Versions["elixir"], defaultElixir)
	assertEqual(t, r.Plan.Sources["elixir"], "default")

	r = generate(t, map[string]string{"mix.exs": "[app: :x]", ".elixir-version": "1.99.0"})
	assertEqual(t, r.Plan.Versions["otp"], maxOTP[defaultElixir])

	r = generate(t, map[string]string{"mix.exs": "[app: :x]", ".erlang-version": "26.2.5"})
	assertEqual(t, r.Plan.Versions["otp"], "26")
	assertEqual(t, r.Plan.Sources["otp"], ".erlang-version")
}

func TestElixirUmbrella(t *testing.T) {
	r := generate(t, map[string]string{
		"mix.exs":                        "defmodule U.MixProject do\n def project, do: [apps_path: \"apps\", releases: [shop: []]]\nend",
		"apps/shop_web/mix.exs":          phoenixMix,
		"apps/shop_web/assets/js/app.js": "",
		"apps/shop/mix.exs":              "defmodule S.MixProject do\n def project, do: [app: :shop]\nend",
	})
	assertEqual(t, r.Plan.Framework, "phoenix")
	assertContains(t, r.Containerfile, "COPY apps apps", "RUN cd apps/shop_web && mix assets.deploy",
		"RUN mix release shop", "rel/shop ./")
	refuteContains(t, r.Containerfile, "COPY lib lib")

	bare := generate(t, map[string]string{"mix.exs": "[apps_path: \"apps\"]", "apps/core/mix.exs": "[app: :core]"})
	if !hasNote(bare.Plan, "releases:") {
		t.Errorf("notes: %q", bare.Plan.Notes)
	}
}

func TestParseMixFile(t *testing.T) {
	mix := ParseMixFile(`defmodule My.MixProject do
  use Mix.Project

  def project do
    [app: :my_app, deps: deps(), aliases: aliases(),
     releases: [web: [applications: [my_app: :permanent]]]]
  end

  # {:commented, "1"}
  defp deps do
    [
      {:phoenix, "~> 1.7"},
      {:heroicons, github: "tailwindlabs/heroicons", app: false},
      {:credo, "~> 1.7", only: [:dev, :test], runtime: false}
    ]
  end

  defp aliases do
    ["assets.deploy": ["esbuild default --minify", "phx.digest"], setup: ["deps.get"]]
  end
end
`)
	assertEqual(t, mix.App, "my_app")
	assertSlice(t, mix.Deps, "phoenix", "heroicons", "credo")
	assertSlice(t, mix.Aliases, "assets.deploy", "setup")
	assertSlice(t, mix.Releases, "web")
	assertEqual(t, mix.Umbrella, false)

	assertEqual(t, ParseMixFile(`[apps_path: "apps"]`).Umbrella, true)
	assertSlice(t, ParseMixFile("  defp aliases do\n    [test: [\"test\"]]").Aliases, "test")
}
