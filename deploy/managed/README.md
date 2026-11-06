# deploy/managed

The managed platform is a graph like any other stack (spec 0015). This folder holds its source, not the files you install:

- [`graph.yaml`](graph.yaml): the platform's own graph. Each platform service is a module of the built in catalog (`catalog/modules/`), bound to the others through the same contract every module uses.
- [`compose.dev.yaml`](compose.dev.yaml): the overlay `task dev:managed` applies. It builds the platform's own images from this checkout and points the ingress at the Vite dev server.
- [`compose.ci.yaml`](compose.ci.yaml): the overlay the `smoke-managed` job applies, with the images tagged `ci`.

## Render the platform folder

```sh
task managed:render
```

This plans `graph.yaml` with the built in catalog and writes the platform folder into `build/managed/`: `docker-compose.yml`, the config templates in `config/`, `setup.sh`, `.env.example`, `README.md` and `graph.yaml`. The release zip packs that folder (`task managed:zip`), so nothing rendered is kept under version control. The `README.md` it writes is the guide for people who install the platform.

To change the platform, you change its catalog entries or `graph.yaml`, never the rendered files. The golden files in `libs/render/testdata/platform/` show each change; review their diff like code.

## Try it

```sh
task dev:managed      # the platform on https://platform.localhost, the editor with hot reload
task smoke:managed    # install, sign in, deploy the sample, remove it (needs the images tagged ci)
```
