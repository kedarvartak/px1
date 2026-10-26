# Hosted dashboard foundation

The hosted dashboard will use Next.js with TypeScript. The route contract in
`internal/hosted/dashboard` is the backend source of truth; frontend route
generation must preserve its organization boundary. `web/dashboard/tokens.css`
maps the static report palette to namespaced tokens so the two products remain
visually related without coupling the account-free Action to a web build.

This foundation intentionally introduces no Node runtime into the Go service.
The deployable frontend, data loaders, and page implementations can therefore
land incrementally behind authenticated APIs.
