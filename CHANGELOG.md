# Changelog

## 1.0.0 (2026-11-12)


### ⚠ BREAKING CHANGES

* the checks no longer bind a required use the graph leaves out. With exactly one matching provide they report the error binding_recommended with that provide as the only candidate, so render and apply refuse the graph until you accept the recommendation in the editor (Accept per use, or Accept all recommendations), which writes an ordinary binding. Binding.auto is removed from the renderer contract, and the editor's "Let the planner choose" option is gone. Every graph the repo ships names its bindings. A stack reviewed before the upgrade gets graph_changed at apply, since the digest no longer covers auto; review it again.
* a running stack is a stack. In api, /api/v1/deployments, /api/v1/deployments/{name} and its operations, review, apply and remove are /api/v1/stacks/..., the operations listDeployments, getDeployment, reviewDeployment, applyDeployment and removeDeployment are listStacks, getStack, reviewStack, applyStack and removeStack, the schemas Deployment, DeploymentSummary, DeploymentPage and DeploymentState are Stack, StackSummary, StackPage and StackState, the list field deployments is stacks, the field project is composeProject, and the events parameter and event deployment are stack. The deploy target serves /v1/stacks/{stack}/apply, remove, state and secrets instead of /v1/projects/{project}/..., with applyStack, removeStack, getStackState, getStackSecrets, StackFile, StackSecret, StackState, ApplyRequest.stack and Operation.stack. The renderer's RenderedProject is RenderedStack with composeProject. The deployer's setting deployments-dir (MP_DEPLOYMENTS_DIR, default {install}/deployments) is stacks-dir (MP_DEPLOYER_STACKS_DIR, default {install}/stacks), the managed label io.github.racherom.modular-platform.deployment is io.github.racherom.modular-platform.stack, and migration 00002 renames the table deployments to stacks with its columns, index and constraints. The editor routes are /stacks and /stacks/$name. Before starting the new version, move <install>/deployments to <install>/stacks (Compose project names mp-<name> do not change, so running containers keep running), then apply each stack once to refresh its labels.
* the plan words are gone from the contracts. POST /v1/plan is POST /v1/check and POST /api/v1/plan is POST /api/v1/check (operation check), the schema PlanResult is CheckResult, Review.plan is Review.check, ApplyRequest.planDigest and Operation.planDigest are digest, and the 409 problem code plan_changed is graph_changed. The Go library libs/planner is libs/check, with check.Graph in place of planner.Plan. The editor keeps its working copy under a new field, so a working copy saved before this version is dropped once.
* the planner service is now the renderer. services/planner is services/renderer (binary /renderer), the catalog module, capability and api slot planner are renderer, the settings MP_PLANNER_MODE, MP_PLANNER_RATE, MP_PLANNER_CLIENTS_DIR, MP_PLANNER_CA_FILE, MP_PLANNER_URL, MP_PLANNER_TOKEN_FILE and MP_PLANNER__* are MP_RENDERER_*, the image ghcr.io/racherom/modular-platform/planner is ghcr.io/racherom/modular-platform/renderer, the online editor routes <base>/renderer/* instead of <base>/planner/*, the contract file contracts/openapi/planner.yaml is renderer.yaml, PlannerError is RendererError and the problem code planner_error is renderer_error.

### Features

* call a running stack a stack in every contract ([1992495](https://github.com/Racherom/modular-platform/commit/1992495ae6c6a291543d97c04335525bf97b4851))
* check a graph instead of planning it, libs/planner becomes libs/check ([30d96ce](https://github.com/Racherom/modular-platform/commit/30d96ceca93845ded1dc5f2c4288f596793819df))
* **contract:** add the v1 capability and module contract on the sample ([8a26bc4](https://github.com/Racherom/modular-platform/commit/8a26bc4a713480d028ed62e078c01160576f8b3a))
* **contract:** check the client of a for_each setup step ([f143782](https://github.com/Racherom/modular-platform/commit/f143782e9b5a07e8190cf6b2bec674590a825508))
* **editor:** add rebind, unbind, selection, inspector and problem marks ([f09c138](https://github.com/Racherom/modular-platform/commit/f09c1381eb844bd45df0610b8dd88ef83c410278))
* **editor:** add tokens, themes, the app shell and drag connect ([66d9ef0](https://github.com/Racherom/modular-platform/commit/66d9ef0a12d8462c6ad95fc9ee97a5c04853a44d))
* **editor:** add undo, redo, tidy layout, sample copy and catalog filter ([e290fba](https://github.com/Racherom/modular-platform/commit/e290fba808ee49f269329df36ec6801d1e1a9460))
* **editor:** complete the design system, showcase route and browser checks ([aed7c6e](https://github.com/Racherom/modular-platform/commit/aed7c6e8e66b97dc6c59268291fad52b6c5850ea))
* **lint:** add the glossary and the retired word check, report only ([95d415c](https://github.com/Racherom/modular-platform/commit/95d415c35dc91598ae0702741c47befdef2d3777))
* **managed:** guard managed projects with an allow list ([61dd80f](https://github.com/Racherom/modular-platform/commit/61dd80f39b1f04c7a2814bd5b8cab7f934bd1129))
* **managed:** live module state, admin reset and the release zip ([8d31a14](https://github.com/Racherom/modular-platform/commit/8d31a142e6c79ac0b63c6ae64400dc73586daca3))
* **managed:** remove, rollback and apply again ([45f68c3](https://github.com/Racherom/modular-platform/commit/45f68c35bf257d9399eb3540efe04302991b2d2b))
* **managed:** run the first managed thread end to end ([50e43aa](https://github.com/Racherom/modular-platform/commit/50e43aa45c959dffd6fcda48f42d0bb3c3bedc39))
* **managed:** serve the platform's own services over TLS from cert-issuer ([447fae7](https://github.com/Racherom/modular-platform/commit/447fae7bcb0b879014bccfb4d525d6f1a55dbc05))
* one Dockerfile per platform image beside its catalog entry, source chosen by build args (spec 0030) ([0c33bcf](https://github.com/Racherom/modular-platform/commit/0c33bcf0901284c3a753afe083a204e22d978f13))
* **online:** build web and renderer from GitHub while the platform is unreleased (spec 0030) ([e215421](https://github.com/Racherom/modular-platform/commit/e2154215943345f244b3fc71d1890616f4b3b3da))
* **planner:** carry graph config values through contract, planner and render ([7f09081](https://github.com/Racherom/modular-platform/commit/7f09081314116f37bb94e4c4114daa3aac5bf252))
* **platform:** render the managed platform from its graph ([ac32bf8](https://github.com/Racherom/modular-platform/commit/ac32bf8dd942126fcbd73bcd215ae919014207e4))
* recommend a provider for a left out use instead of binding it ([bc8f0a5](https://github.com/Racherom/modular-platform/commit/bc8f0a58a0592ab79e407973b7f97d76f03d26b7))
* rename the planner service to the renderer ([9193f86](https://github.com/Racherom/modular-platform/commit/9193f86cfa5edad97c719996e16d02b8188e7e26))
* **render:** a zip builds the unreleased platform images it names from the renderer's commit (spec 0030) ([7fcabb2](https://github.com/Racherom/modular-platform/commit/7fcabb2debb6f0389d4c411d4540a641b559faeb))
* **render:** add the mp_jobs wait anchor and egress for consumers of external instances ([feeab26](https://github.com/Racherom/modular-platform/commit/feeab268925cb5258bbb21c746a07e981d4c8b97))
* **render:** add the revoke job per provisioner provide ([f0118e6](https://github.com/Racherom/modular-platform/commit/f0118e667bc3cc7150769c17e41d7a3d318484b7))
* **render:** admit only NET_BIND_SERVICE on deployed graphs and require cap_drop and no-new-privileges ([bed25c6](https://github.com/Racherom/modular-platform/commit/bed25c6189085a5577822cf1a53c305b7f9f1e4b))
* **render:** ask values and run setup steps in setup.sh ([e1cfacb](https://github.com/Racherom/modular-platform/commit/e1cfacbb4472ba172bc9356f7ad5b5c756fa01ff))
* **render:** issue a certificate for postgres through the certificate provisioner ([3c33be0](https://github.com/Racherom/modular-platform/commit/3c33be0f769afa809f0d97baf4a182f0c2bc3485))
* **render:** refuse host access and setup in deployed graphs, render publish and host mounts ([3607dc0](https://github.com/Racherom/modular-platform/commit/3607dc0af1f2e5eac6f62d3723b761a5de5de90b))
* **render:** render every non sdk config value as a .env variable and check .env in setup.sh ([d2b92c1](https://github.com/Racherom/modular-platform/commit/d2b92c12d74e8b7a9604c00d3bb502566175089f))
* **render:** render external provisioners, client files, egress and module environment ([3fa1d31](https://github.com/Racherom/modular-platform/commit/3fa1d31789ed21a392cd91639a0b02c1478ac406))
* **render:** render module templates into config/&lt;instance&gt;/ ([005c5aa](https://github.com/Racherom/modular-platform/commit/005c5aa3ba2e9e4a4d40ad0d15a9fda3af39c4ed))
* **render:** the managed zip builds its unreleased platform images and never pulls them (spec 0030) ([d224900](https://github.com/Racherom/modular-platform/commit/d224900e27f692a03f7c289f8bf1b0e70279ce7f))
* **sdk:** add binding clients, readiness and the sdk render ([513ff9d](https://github.com/Racherom/modular-platform/commit/513ff9d34fab234415191bf357cb592dc3d076a3))
* **sdk:** add host names, ACME TLS and the data clients ([3eb5712](https://github.com/Racherom/modular-platform/commit/3eb57123d4371282faaffe7d2036df5868dbef29))
* **sdk:** add the start, push, caller and binding sources ([cdb03b6](https://github.com/Racherom/modular-platform/commit/cdb03b6daf21725be88749003be58afcefd8a95c))
* **sdk:** mint and check the MP-Caller token ([63e39ff](https://github.com/Racherom/modular-platform/commit/63e39ff2ccdc4906fc27f9263e510772386be205))
* **sdk:** replace autocert with the sdk's own ACME client ([3cab6fc](https://github.com/Racherom/modular-platform/commit/3cab6fc62b29c0588702e5d0cd9c51106355d244))
* **sdk:** run the planner on a thin module sdk ([3244fde](https://github.com/Racherom/modular-platform/commit/3244fdeea65c89ddee5933889cab61323a807806))
* **sdk:** typed getters for slots with no client, planner, deployer and api on them ([a072e6c](https://github.com/Racherom/modular-platform/commit/a072e6c5e95fd055db77fde5da9098cf3e071e9b))
* **web:** add a module from the catalog in the editor ([eb99734](https://github.com/Racherom/modular-platform/commit/eb99734039b10823d32c4991d9bdab4c3ed24b7c))
* **web:** set config values in the inspector and route the paper platform's site root by graph ([937ada2](https://github.com/Racherom/modular-platform/commit/937ada24c881ef891df7967433f33be959533cdf))


### Bug Fixes

* apply the scaffold review fixes ([d943ed8](https://github.com/Racherom/modular-platform/commit/d943ed83756c77b2075e1d23dc59f152c023b7fb))
* **ci:** a gate job carries the required release images check (spec 0030) ([9351acf](https://github.com/Racherom/modular-platform/commit/9351acfcff353161c74dd0f0911366d6f9234d5e))
* **editor:** keep the focus ring and edge stroke above React Flow's styles ([2817190](https://github.com/Racherom/modular-platform/commit/2817190ea46b24d582303ff9b6fad86cc8d17551))
* retired routes answer 404 everywhere (spec 0027, AC-6) ([9b5be57](https://github.com/Racherom/modular-platform/commit/9b5be57e8dc0bd50debb0f107232fab5b078e648))
* **web:** center the new node after the sample handoff (spec 0029, AC-11) ([7470ca5](https://github.com/Racherom/modular-platform/commit/7470ca5cc2eba8a58dfd762399a79a820b9a6203))
