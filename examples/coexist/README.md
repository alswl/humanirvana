# Nirvana Showcase：Nirvana/Huma 合成示例

A synthetic in-memory project/task/message service for exercising locked Nirvana v0.3.1 alongside Huma. It is a runnable feature sample, not a real application migration acceptance test. The current legacy path uses `humanirvana.RegisterLegacy(builder, descriptors)` on an application-owned Nirvana builder; native routes use `huma.Register` directly. Each `showcase.New()` creates separate equivalent Original and adapted stores. Tests construct fresh apps, so state and writes are isolated between cases.

## 构建与启动

从仓库根目录使用固定 Go 1.23.12 安装路径。下面的构建命令可直接复制；`127.0.0.1:0` 由系统分配动态端口，进程会打印实际地址。把后续 curl 中的 `PORT` 替换成该端口。

```sh
go build -o ./nirvana-showcase ./examples/coexist/cmd
./nirvana-showcase -listen 127.0.0.1:0 -arm original
# 或：./nirvana-showcase -listen 127.0.0.1:0 -arm adapted
# 或：./nirvana-showcase -listen 127.0.0.1:0 -arm composed
# 或：./nirvana-showcase -listen 127.0.0.1:0 -arm cutover -owner original
```

The CLI binds before serving and prints the actual address. `127.0.0.1:0` asks the OS for a free port. Stop with Ctrl-C. Modes:

- `original`: original Nirvana descriptors and middleware.
- `adapted`: Huma API with the same legacy descriptor handlers registered by `RegisterLegacy`.
- `composed`: adapted legacy routes plus additive Huma-native health and async-status routes.

`build` and `native` remain aliases for `adapted` and `composed`. `cutover` assigns only `POST /projects/{projectID}/tasks` to the selected owner; every other request goes to Original. `-owner adapted` selects the Huma-backed `RegisterLegacy` path. `-owner` is valid only with `-arm cutover`. To switch or roll back, stop the process and restart with the other owner; there is no hot update or production gateway. Each process creates independent Original and adapted in-memory stores, so a restart resets both stores. The selector changes request ownership only; it does not migrate or reconcile application state.

The original-only routes under `/showcase` deliberately do not appear in Build: they use Nirvana `Auto`, `Form`, `File`, and `Prefab` parameter sources that pilot rejects at Build startup. Their absence from Build is covered by explicit 404 assertions; rejection is covered by a startup-validation test.

## 请求示例

以下第一组请求须在 fresh process 的 `-arm original`，或 `-arm cutover -owner original` 模式下按顺序执行；示例依赖初始任务与 job ID。`adapted`/`composed` 及 `cutover -owner adapted` 使用各自独立的内存 store，不能直接照此整段执行。legacy project subtree requires `Authorization: Bearer showcase-token`:

```sh
curl -i -H 'Authorization: Bearer showcase-token' -H 'X-Demo-Policy: allow' http://127.0.0.1:PORT/projects/1
curl -i -H 'Authorization: Bearer showcase-token' http://127.0.0.1:PORT/projects/1/summary
curl -i -H 'Authorization: Bearer showcase-token' -H 'Accept: application/xml' http://127.0.0.1:PORT/projects/1/representation
curl -i -H 'Authorization: Bearer showcase-token' -H 'Content-Type: application/json' \
  -d '{"title":"ship demo"}' http://127.0.0.1:PORT/projects/1/tasks
curl -i -H 'Authorization: Bearer showcase-token' 'http://127.0.0.1:PORT/projects/1/tasks?limit=1'
curl -i -H 'Authorization: Bearer showcase-token' -H 'Content-Type: application/json' \
  -d '{"text":"ready"}' http://127.0.0.1:PORT/tasks/2/messages
curl -i -H 'Authorization: Bearer showcase-token' http://127.0.0.1:PORT/tasks/2
curl -i -H 'Authorization: Bearer showcase-token' -H 'Content-Type: application/json' -H 'Accept: application/json' -d '{"title":"cutover task"}' http://127.0.0.1:PORT/projects/1/tasks
curl -i -H 'Authorization: Bearer showcase-token' -H 'Content-Type: application/json' -H 'Accept: application/json' -d '{"title":"revised task","state":"done"}' -X PUT http://127.0.0.1:PORT/tasks/1
curl -i -H 'Authorization: Bearer showcase-token' -H 'Content-Type: text/plain' -H 'Accept: text/plain, application/json' --data-binary 'hello' -X PUT http://127.0.0.1:PORT/tasks/1/text
curl -i -X POST -H 'Authorization: Bearer showcase-token' -H 'Accept: application/json' http://127.0.0.1:PORT/tasks/1/async
curl -i -H 'Authorization: Bearer showcase-token' -H 'Accept: application/json' http://127.0.0.1:PORT/jobs/1
curl -i -H 'Content-Type: application/json' -H 'Accept: application/json' -d '{}' -X PUT http://127.0.0.1:PORT/jobs/1/complete
curl -i -H 'Authorization: Bearer showcase-token' -H 'Accept: application/json' http://127.0.0.1:PORT/jobs/1
curl -i -H 'Authorization: Bearer showcase-token' -H 'Content-Type: application/json' -H 'Accept: application/json' -d '{}' -X DELETE http://127.0.0.1:PORT/tasks/1
```

仅 Original 可用的 `Auto` context probe 与 multipart 上传：

```sh
curl -i -H 'X-Request-ID: demo-17' http://127.0.0.1:PORT/showcase/projects/1/context
curl -i http://127.0.0.1:PORT/showcase/projects/1/scope
curl -i http://127.0.0.1:PORT/modifier/1
curl -i -F 'label=design' -F 'file=@README.md;type=text/markdown' \
  http://127.0.0.1:PORT/showcase/projects/1/attachments
```

The `X-Demo-Policy: allow` header is required for `GET /projects/1`; the command above supplies it. To see the policy rejection explicitly, run `curl -i -H 'Authorization: Bearer showcase-token' http://127.0.0.1:PORT/projects/1` without the policy header; expect 403 before the handler. JSON body endpoints require explicit Content-Type. In `TestAsyncCreateJobStateTransitionOriginalAndBuild`, async-create sends an empty request body with no Content-Type because the operation declares no request body; job-complete sends `{}` as `application/json`. This does not establish bodyless parity for DELETE or job-complete.

Cutover and rollback are restart-based. Example: start with `-arm cutover -owner original`, stop with Ctrl-C, then start with `-arm cutover -owner adapted`; rollback by restarting with `-owner original`. Both owners use equivalent but separate in-memory stores, and restart resets all demo state.

`composed` adds routes without external infrastructure:

```sh
curl -i http://127.0.0.1:PORT/native/health
curl -i http://127.0.0.1:PORT/native/version
curl -i http://127.0.0.1:PORT/native/async/job-7
```

The `/native/async/{id}` endpoint reports a deterministic completed fixture; it is not an async acceptance test. For a fresh process, the real Nirvana job flow is `/tasks/1/async` → read the returned job ID at `/jobs/{jobID}` (queued) → `PUT /jobs/{jobID}/complete` with `{}` and `Content-Type: application/json` → read again (completed). Both Original and Build transitions are asserted in `TestAsyncCreateJobStateTransitionOriginalAndBuild`. Data is in memory and resets when the process restarts.

`RouteCutover` is an in-memory exact operation selector for tests and demonstrations. `TestPerOperationCutoverRollbackHasSingleOwner` assigns `POST /projects/{projectID}/tasks` original→Build→original and checks retained `/legacy/status`, isolated stores, exact handler/write counts, and no replay. CLI selection occurs at process start; it is a demonstration selector, not a production deployment router.

## Feature and evidence map

Official links below are the supplied Nirvana Chinese documentation chapters. API names and behavior are checked against the repository's locked `github.com/caicloud/nirvana v0.3.1` module source; where the page and locked source differ, the locked source governs.

| Feature | Official chapter | v0.3.1 source API | Endpoint and executable evidence | Original / Build |
| --- | --- | --- | --- | --- |
| Descriptor path inheritance, shared handler CRUD, JSON | [API guide](https://caicloud.github.io/nirvana/zh-hans/guides/api.html) | `definition.Descriptor`, `definition.Definition`, `service.FirstContextParameter`, `service.ConsumeNoneForHTTPGet` | projects/tasks/messages endpoints; `TestOriginalAndBuildSharedHandlerDomainParityAndSingleWrites`, `TestTaskUpdateDeleteSemanticsAndOriginalBuildParity` | pass / pass |
| Single-operation ownership switch and rollback | [API guide](https://caicloud.github.io/nirvana/zh-hans/guides/api.html) | showcase `RouteCutover` keyed by method plus path | `POST /projects/{projectID}/tasks`; `TestPerOperationCutoverRollbackHasSingleOwner`; CLI `-arm cutover -owner original|adapted` | original→Build→original pass / pass; restart selected; other routes Original |
| Inherited middleware ordering and auth short circuit | [Middleware](https://caicloud.github.io/nirvana/zh-hans/concepts/middleware.html) | `definition.Middleware`, `definition.Chain`, `service.HTTPContextFrom` | authenticated project/task endpoints; `TestAuthAndValidationFailuresDoNotWriteOrReplay` and `App.Trace` | pass / pass |
| Custom converter and validator operators | [Operator](https://caicloud.github.io/nirvana/zh-hans/concepts/operator.html) | `definition.OperatorFunc`, `definition.Parameter.Operators` | `POST /projects/1/tasks`; `TestOriginalAndBuildSharedHandlerDomainParityAndSingleWrites` asserts uppercase/space normalization; blank title rejection asserts zero handler calls | pass / pass |
| Built-in struct validation | [Operator](https://caicloud.github.io/nirvana/zh-hans/concepts/operator.html) | `operators/validator.Struct`, `validate:"required,min=3"` | task create accepts normalized title and rejects blank before handler; reserved `internal:` prefix is rejected by the custom operator | pass / pass |
| Data and Meta result destinations | [Destination](https://caicloud.github.io/nirvana/zh-hans/concepts/destination.html) | `definition.DataResultFor`, `definition.MetaResultFor`, `definition.ErrorResult`, `service.MetaDestinationHandler` | `GET /projects/1/summary`; `TestMetaDestinationSetsHeadersInOriginalAndBuild` checks JSON response plus `X-Project-ID` and `X-Project-Name` | pass / pass |
| JSON/XML response negotiation | [Destination](https://caicloud.github.io/nirvana/zh-hans/concepts/destination.html) | `Definition.Produces`, `service.AcceptTypes`, registered data producers | `GET /projects/1/representation`; `TestAcceptNegotiatesJSONAndXMLInOriginalAndBuild` sends both Accept values to both runtimes | pass / pass |
| Prefab request scope and application singleton | [Prefab](https://caicloud.github.io/nirvana/zh-hans/concepts/prefab.html) | `service.Prefab`, `service.RegisterPrefab`, `service.PrefabParameterGenerator` | scope endpoint; `TestPrefabRequestScopeAndSingletonInOriginal`, `TestPilotRejectsOriginalOnlySourcesAtBuildStartup` | pass / Build startup rejects |
| Auto parameter selection across path and header | [Operator](https://caicloud.github.io/nirvana/zh-hans/concepts/operator.html) | `Definition.Source=Auto`, `source` tags, `AutoParameterGenerator` | context endpoint; `TestOriginalOnlyAutoContextAndMultipartAttachment`, startup rejection test | pass / Build startup rejects |
| Form and File multipart inputs | [Parameter](https://caicloud.github.io/nirvana/zh-hans/concepts/parameter.html) | form/file parameter helpers, `ParseRequestForm`, generators | attachment endpoint; `TestOriginalOnlyAutoContextAndMultipartAttachment`, startup rejection test | pass / Build startup rejects |
| Request context value, liveness, deadline propagation | [Context](https://caicloud.github.io/nirvana/zh-hans/concepts/context.html) | handler `context.Context`, `context.WithTimeout` in descriptor middleware | context endpoint; `TestOriginalOnlyAutoContextAndMultipartAttachment` | value/live/deadline pass / Build startup rejects Auto source; disconnect cancellation not tested |
| Nirvana Filters: request parsing and policy | [Filter](https://caicloud.github.io/nirvana/zh-hans/concepts/filter.html) | `ParseRequestForm` and custom policy Filter | attachment and `GET /projects/1`; `TestOriginalOnlyAutoContextAndMultipartAttachment`, `TestNirvanaFilterShortCircuitsBeforeRouting` | pass / only native adapter filters; custom Nirvana Filter not adapted |
| Definition Modifier | [API guide](https://caicloud.github.io/nirvana/zh-hans/guides/api.html) | original-only custom `DefinitionModifier` | `/modifier/{projectID}`; `TestDefinitionModifierChangesRuntimeMetadata` | pass / only native adapter modifiers; Nirvana modifier not adapted |
| Text/plain consumer, producer, custom result operator | [Operator](https://caicloud.github.io/nirvana/zh-hans/concepts/operator.html) | Nirvana body consumer/producer and result operator | text endpoint; `TestTextPlainConsumerProducerAndResultOperatorBothArms` | pass / pass |
| Real Nirvana asynchronous job transition | [API guide](https://caicloud.github.io/nirvana/zh-hans/guides/api.html) | `definition.AsyncCreate`, job handlers | async-create/read/complete endpoints; `TestAsyncCreateJobStateTransitionOriginalAndBuild` | queued→completed pass / pass |
| Additive health and version routes | [API guide](https://caicloud.github.io/nirvana/zh-hans/guides/api.html) | Huma `huma.Register`, `NativeModule`, `RouteClaim` | `/native/health`, `/native/version`; `TestNativeEnhancementIsAdditiveAndDocumented` | no Original equivalent / pass composed; not Nirvana plugins |
| Synthetic native async status | [API guide](https://caicloud.github.io/nirvana/zh-hans/guides/api.html) | Huma native operation | `/native/async/{id}`; `TestNativeEnhancementIsAdditiveAndDocumented` | no Original equivalent / fixture only; not async acceptance |
| Unmigrated original endpoint retained | [API guide](https://caicloud.github.io/nirvana/zh-hans/guides/api.html) | original descriptor with `definition.Get` | `/legacy/status`; auth/rejection test verifies it remains callable | pass / intentionally absent |

## Current gaps

The feature table records demonstrated features. Build intentionally rejects Auto/Form/File/Prefab sources at startup, and the general pilot only supports its native adapter Filter/Modifier behavior; this sample does not claim it adapts Nirvana custom Filters or DefinitionModifiers. Native health/version endpoints are additive Huma routes, not Nirvana plugins. `/native/async/{id}` is a static fixture; the separate Nirvana job flow provides the actual queued-to-completed transition. Empty-body parity is not claimed: parity tests send explicit JSON body/content type for bodyless DELETE, async-create, and completion operations. Response panic/committed-write boundaries and external telemetry are outside this demo. Cutover has no hot update or production gateway role, and the two in-memory stores are independent.

The adapter's original-vs-Build tests use the same handler functions and compare response/status/content type while asserting one write per arm. Rejection tests assert no extra write or handler replay. The original-only upload test asserts one in-memory attachment. Each test creates a fresh app, preventing cross-test writes.
