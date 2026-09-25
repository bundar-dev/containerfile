package containerfile

import (
	"encoding/json"
	"testing"
)

func pkg(v map[string]any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

type m = map[string]any

func TestNodeNextStandalone(t *testing.T) {
	r := generate(t, map[string]string{
		"package.json": pkg(m{"packageManager": "pnpm@9.12.0", "scripts": m{"build": "next build", "start": "next start"},
			"dependencies": m{"next": "15", "@prisma/client": "5"}, "engines": m{"node": ">=20.9"}}),
		"pnpm-lock.yaml": "lockfileVersion: '9.0'", "next.config.mjs": "export default { output: 'standalone' }",
		"prisma/schema.prisma": "", "public/favicon.ico": "",
	})
	p := r.Plan
	assertEqual(t, p.Framework, "nextjs")
	assertEqual(t, p.PackageManager, "pnpm")
	assertEqual(t, p.Versions["node"], "20")
	assertContains(t, r.Containerfile, "ARG NODE_VERSION=20", "RUN npm install -g pnpm@9.12.0",
		"COPY package.json pnpm-lock.yaml ./", "RUN pnpm install --frozen-lockfile --prod=false",
		"RUN pnpm exec prisma generate", "RUN pnpm run build", "/app/.next/standalone /app", "/app/public /app/public",
		`HOSTNAME="0.0.0.0"`, `CMD ["node", "server.js"]`, "openssl")
	refuteContains(t, r.Containerfile, "prune")
}

func TestNodeNextServer(t *testing.T) {
	r := generate(t, map[string]string{
		"package.json":      pkg(m{"scripts": m{"start": "next start"}, "dependencies": m{"next": "15"}}),
		"package-lock.json": "{}",
	})
	if !hasNote(r.Plan, "standalone") {
		t.Error("expected standalone note")
	}
	assertContains(t, r.Containerfile, "RUN npm prune --omit=dev", `CMD ["npm", "run", "start"]`)
}

func TestNodeViteSPA(t *testing.T) {
	r := generate(t, map[string]string{
		"package.json":      pkg(m{"scripts": m{"dev": "vite", "build": "vite build"}, "devDependencies": m{"vite": "5"}}),
		"package-lock.json": "{}", ".nvmrc": "lts/iron",
	})
	assertEqual(t, r.Plan.Framework, "vite")
	assertEqual(t, r.Plan.Port, 8080)
	assertEqual(t, len(r.Plan.Env), 0)
	assertContains(t, r.Containerfile, "ARG NODE_VERSION=lts", "nginx-unprivileged",
		"COPY --from=build /app/dist /usr/share/nginx/html", "try_files $uri $uri/ /index.html")
	refuteContains(t, r.Containerfile, "prune", "CMD")

	server := generate(t, map[string]string{"package.json": pkg(m{"scripts": m{"start": "node server.js"}, "dependencies": m{"vite": "5"}})})
	assertEqual(t, server.Plan.Framework, "")
}

func TestNodeExpress(t *testing.T) {
	r := generate(t, map[string]string{
		"package.json": pkg(m{"main": "server.js", "dependencies": m{"express": "4"}}),
		"yarn.lock":    "", "server.js": "const port = process.env.PORT || 8081\napp.listen(port)",
	})
	assertEqual(t, r.Plan.PackageManager, "yarn")
	assertEqual(t, r.Plan.Port, 8081)
	assertContains(t, r.Containerfile, "RUN yarn install --frozen-lockfile --production=false",
		"RUN yarn install --production=true --frozen-lockfile", `CMD ["node", "server.js"]`, "USER node")

	entry := generate(t, map[string]string{"package.json": "{}", "index.js": ""})
	assertSlice(t, entry.Plan.Cmd, "node", "index.js")
	assertEqual(t, entry.Plan.Port, 3000)
	assertContains(t, entry.Containerfile, "RUN npm install --include=dev")
}

func TestNodeBun(t *testing.T) {
	r := generate(t, map[string]string{
		"package.json": pkg(m{"scripts": m{"start": "bun run index.ts"}}), "bun.lock": "", ".tool-versions": "bun 1.2.3\n",
	})
	assertEqual(t, r.Plan.Runtime, "bun")
	assertContains(t, r.Containerfile, "ARG BUN_VERSION=1.2.3", "FROM docker.io/oven/bun:${BUN_VERSION}-slim AS base",
		"RUN bun install --frozen-lockfile", `CMD ["bun", "run", "start"]`, "USER bun", "--chown=bun:bun")

	field := generate(t, map[string]string{"package.json": pkg(m{"packageManager": "bun@1.1.0"})})
	assertEqual(t, field.Plan.Versions["bun"], "1")
	assertContains(t, field.Containerfile, "RUN bun install\n")

	def := generate(t, map[string]string{"package.json": "{}", "bun.lockb": ""})
	assertEqual(t, def.Plan.Versions["bun"], "1")
	assertEqual(t, def.Plan.Sources["bun"], "default")
}

func TestNodeYarnBerry(t *testing.T) {
	r := generate(t, map[string]string{
		"package.json": pkg(m{"packageManager": "yarn@4.1.0", "scripts": m{"start": "node .", "build": "tsc"}}),
		"yarn.lock":    "", ".yarnrc.yml": "",
	})
	assertEqual(t, r.Plan.PackageManager, "yarn-berry")
	assertContains(t, r.Containerfile, "corepack enable", "RUN yarn install --immutable", "RUN yarn run build",
		`CMD ["yarn", "run", "start"]`)
	refuteContains(t, r.Containerfile, "prune")
}

func TestNodePackageManagerField(t *testing.T) {
	cases := map[string]string{"pnpm@9.0.0": "pnpm", "yarn@1.22.0": "yarn", "yarn@3.6.0": "yarn-berry", "deno@2": "npm"}
	for field, want := range cases {
		assertEqual(t, generate(t, map[string]string{"package.json": pkg(m{"packageManager": field})}).Plan.PackageManager, want)
	}
}

func TestNodePnpmLockfileVersions(t *testing.T) {
	for lock, want := range map[string]string{"5.4": "7", "'6.0'": "8", "'9.0'": "latest"} {
		r := generate(t, map[string]string{"package.json": "{}", "pnpm-lock.yaml": "lockfileVersion: " + lock + "\n"})
		assertContains(t, r.Containerfile, "RUN npm install -g pnpm@"+want, "RUN pnpm prune --prod")
	}
}

func TestNodeWorkspaces(t *testing.T) {
	r := generate(t, map[string]string{
		"package.json":      pkg(m{"workspaces": []string{"packages/*"}, "scripts": m{"start": "node ."}}),
		"package-lock.json": "{}",
	})
	assertContains(t, r.Containerfile, "COPY . .\nRUN npm ci --include=dev")
}

func TestNodeFrameworks(t *testing.T) {
	astro := generate(t, map[string]string{"package.json": pkg(m{"dependencies": m{"astro": "4", "@astrojs/node": "8"}})})
	assertEqual(t, astro.Plan.Port, 4321)
	assertContains(t, astro.Containerfile, `CMD ["node", "./dist/server/entry.mjs"]`)

	static := generate(t, map[string]string{"package.json": pkg(m{"dependencies": m{"astro": "4"}})})
	assertEqual(t, static.Plan.Assigns["staticDir"], "dist")

	nuxt := generate(t, map[string]string{"package.json": pkg(m{"scripts": m{"build": "nuxt build"}, "dependencies": m{"nuxt": "3"}})})
	assertContains(t, nuxt.Containerfile, "/app/.output /app/.output", `CMD ["node", ".output/server/index.mjs"]`)

	nest := generate(t, map[string]string{"package.json": pkg(m{"dependencies": m{"@nestjs/core": "10"}})})
	assertSlice(t, nest.Plan.Cmd, "node", "dist/main")

	remix := generate(t, map[string]string{"package.json": pkg(m{"scripts": m{"start": "remix-serve"}, "dependencies": m{"@remix-run/node": "2"}})})
	assertEqual(t, remix.Plan.Framework, "remix")

	cra := generate(t, map[string]string{"package.json": pkg(m{"scripts": m{"start": "react-scripts start"}, "dependencies": m{"react-scripts": "5"}})})
	assertEqual(t, cra.Plan.Framework, "create-react-app")
	assertEqual(t, cra.Plan.Port, 8080)

	svelte := generate(t, map[string]string{"package.json": pkg(m{"devDependencies": m{"@sveltejs/kit": "2"}})})
	assertSlice(t, svelte.Plan.Cmd, "node", "build")
	if !hasNote(svelte.Plan, "adapter-node") {
		t.Error("expected adapter-node note")
	}
	svelteStatic := generate(t, map[string]string{"package.json": pkg(m{"devDependencies": m{"@sveltejs/kit": "2", "@sveltejs/adapter-static": "3"}})})
	assertEqual(t, svelteStatic.Plan.Assigns["staticDir"], "build")
	if hasNote(svelteStatic.Plan, "adapter-node") {
		t.Error("unexpected adapter-node note")
	}
}

func TestNodeNoEntry(t *testing.T) {
	r := generate(t, map[string]string{"package.json": "not json"})
	assertEqual(t, len(r.Plan.Cmd), 0)
	if !hasNote(r.Plan, "set CMD manually") {
		t.Error("expected CMD note")
	}
	plan, err := Scan(project(t, nil), Options{Runtime: "node"})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, plan.PackageManager, "npm")
}
