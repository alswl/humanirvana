# Nirvana Showcase：Nirvana/Huma 合成示例

A synthetic in-memory project/task/message service for exercising locked Nirvana v0.3.1 alongside Huma. It is a runnable feature sample, not a real application migration acceptance test. The current legacy path uses `humanirvana.RegisterLegacy(builder, descriptors)` on an application-owned Nirvana builder; native routes use `huma.Register` directly. Each `showcase.New()` creates separate equivalent Original and adapted stores. Tests construct fresh apps, so state and writes are isolated between cases.

## Runtime arms and migration boundary

| Arm | Runtime | Business state | What it demonstrates |
| --- | --- | --- | --- |
| `original` | Unmodified Nirvana v0.3.1 REST builder | Original store | Existing descriptors, handler, Filters, middleware and modifiers. |
| `adapted` | Same Nirvana builder API plus `humanirvana.NewAdapter` and `RegisterLegacy` | Separate equivalent store | Legacy descriptors retain Nirvana's execution chain through the adapter. It does not make these endpoints Huma typed operations. |
| `native` | Standard Huma `humago` adapter and `huma.Register` | Equivalent isolated store | Independent upstream Huma HTTP baseline for typed operation and middleware behavior. |
| `composed` | `huma.NewAPI(..., humanirvana.NewAdapter(builder))`, `huma.Register`, and `RegisterLegacy` on one Nirvana builder/service | Adapted store | Typed Huma operation and legacy descriptor coexist on the same Nirvana router. |
| `cutover` | Request-time selector over original and RegisterLegacy handlers | Separate equivalent stores | One explicitly assigned method/path can be switched and rolled back without replaying a request. Unassigned routes remain Original. Restart resets both stores. |

The Huma adapter registers each standard typed handler callback through a context-aware Nirvana descriptor. Nirvana performs route/media admission, then the Huma callback performs typed binding, validation and response serialization. In `composed`, `huma.Register` operations and legacy descriptors share the same builder/service/router. `native` remains a separate upstream Huma baseline for paired comparisons. The sample is synthetic and does not satisfy real-application acceptance.

## 构建与启动

从仓库根目录使用固定 Go 1.23.12 安装路径。下面的构建命令可直接复制；`127.0.0.1:0` 由系统分配动态端口，进程会打印实际地址。把后续 curl 中的 `PORT` 替换成该端口。

```sh
go build -o ./nirvana-showcase ./examples/coexist/cmd
./nirvana-showcase -listen 127.0.0.1:0 -arm original
# 或：./nirvana-showcase -listen 127.0.0.1:0 -arm adapted
# 或：./nirvana-showcase -listen 127.0.0.1:0 -arm native
# 或：./nirvana-showcase -listen 127.0.0.1:0 -arm composed
# 或：./nirvana-showcase -listen 127.0.0.1:0 -arm cutover -owner original
```

The CLI binds before serving and prints the actual address. `127.0.0.1:0` asks the OS for a free port. Stop with Ctrl-C. Modes:

- `original`: original Nirvana descriptors and middleware.
- `adapted`: Huma API with the same legacy descriptor handlers registered by `RegisterLegacy`.
- `native`: standard Huma typed health/version/status routes on a separate `humago` router.
- `composed`: typed Huma operations plus legacy descriptors on the same Nirvana router.

`build` remains an alias for `adapted`. `cutover` assigns only `POST /projects/{projectID}/tasks` to the selected owner; every other request goes to Original. `-owner adapted` selects the `RegisterLegacy` path. `-owner` is valid only with `-arm cutover`. To switch or roll back, stop the process and restart with the other owner; there is no hot update or production gateway. Each process creates independent Original and adapted in-memory stores, so a restart resets both stores. The selector changes request ownership only; it does not migrate or reconcile application state.

The `/showcase` routes (Auto, Prefab, Form/File) and `/modifier/{projectID}` are registered on both runtimes: Original uses plain `builder.AddDescriptor`, adapted uses `RegisterLegacy` on its own builder with the same modifiers. Because `RegisterLegacy` keeps Nirvana's own execution chain, these parameter sources need no adapter support.

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

`Auto` context probe、Prefab 与 multipart 上传（两种运行时都可用）：

```sh
curl -i -H 'X-Request-ID: demo-17' http://127.0.0.1:PORT/showcase/projects/1/context
curl -i http://127.0.0.1:PORT/showcase/projects/1/scope
curl -i http://127.0.0.1:PORT/modifier/1
curl -i -F 'label=design' -F 'file=@README.md;type=text/markdown' \
  http://127.0.0.1:PORT/showcase/projects/1/attachments
```

The `X-Demo-Policy: allow` header is required for `GET /projects/1`; the command above supplies it. To see the policy rejection explicitly, run `curl -i -H 'Authorization: Bearer showcase-token' http://127.0.0.1:PORT/projects/1` without the policy header; expect 403 before the handler. JSON body endpoints require explicit Content-Type. In `TestAsyncCreateJobStateTransitionOriginalAndBuild`, async-create sends an empty request body with no Content-Type because the operation declares no request body; job-complete sends `{}` as `application/json`. This does not establish bodyless parity for DELETE or job-complete.

Cutover and rollback are restart-based. Example: start with `-arm cutover -owner original`, stop with Ctrl-C, then start with `-arm cutover -owner adapted`; rollback by restarting with `-owner original`. Both owners use equivalent but separate in-memory stores, and restart resets all demo state.

### 测试观测入口（`-observe`）

`-observe` is off by default. It is test-only demo wiring for black-box checks, not a humanirvana library API and not a production control plane. When enabled, the CLI reserves `/_demo/`:

- `GET /_demo/observe` returns `{"original":{...},"adapted":{...}}`; each store reports `task_writes`, `message_writes`, `handler_calls` and `trace`. In `cutover` it also reports `cutover_owner`. `adapted`, `composed`, `native`, and the adapted cutover owner share the adapted store.
- `POST /_demo/cutover?owner=original|adapted` switches the single cutover route in the running process and returns the same observation. Unknown owners return 400; arms other than `cutover` return 404. Other methods return 405.

`trace` holds the most recent request that entered the audit middleware (`audit.before`, `auth`, `handler` for write handlers, `audit.after`) or the Huma group middleware (`native.before`, `handler`, `native.after`). Read handlers record no `handler` event. Requests rejected by the request policy Filter stop before middleware, so `trace` and `handler_calls` stay unchanged. `handler_calls` counts project/task/message handlers and `GET /native/health`; job reads/completion and other native routes are not counted.

```sh
./nirvana-showcase -listen 127.0.0.1:0 -arm cutover -owner original -observe
curl -s http://127.0.0.1:PORT/_demo/observe
curl -s -X POST 'http://127.0.0.1:PORT/_demo/cutover?owner=adapted'
```

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
| Prefab request scope and application singleton | [Prefab](https://caicloud.github.io/nirvana/zh-hans/concepts/prefab.html) | `service.Prefab`, `service.RegisterPrefab`, `service.PrefabParameterGenerator` | scope endpoint; `TestPrefabRequestScopeAndSingletonInBothArms` | pass / pass |
| Auto parameter selection across path and header | [Operator](https://caicloud.github.io/nirvana/zh-hans/concepts/operator.html) | `Definition.Source=Auto`, `source` tags, `AutoParameterGenerator` | context endpoint; `TestAutoContextAndMultipartAttachmentInBothArms` | pass / pass |
| Form and File multipart inputs | [Parameter](https://caicloud.github.io/nirvana/zh-hans/concepts/parameter.html) | form/file parameter helpers, `ParseRequestForm`, generators | attachment endpoint; `TestAutoContextAndMultipartAttachmentInBothArms` | pass / pass |
| Request context value, liveness, deadline propagation | [Context](https://caicloud.github.io/nirvana/zh-hans/concepts/context.html) | handler `context.Context`, `context.WithTimeout` in descriptor middleware | context endpoint; `TestAutoContextAndMultipartAttachmentInBothArms` | value/live/deadline pass / pass; disconnect cancellation not tested |
| Nirvana Filters: request parsing and policy | [Filter](https://caicloud.github.io/nirvana/zh-hans/concepts/filter.html) | `ParseRequestForm` and custom policy Filter | attachment and `GET /projects/1`; `TestAutoContextAndMultipartAttachmentInBothArms`, `TestRequestPolicyFilterMatchesOriginalAndAdaptedBeforeHandler` | pass / pass (same Filters installed on both builders) |
| Definition Modifier | [API guide](https://caicloud.github.io/nirvana/zh-hans/guides/api.html) | custom `DefinitionModifier` on its own builder | `/modifier/{projectID}`; `TestDefinitionModifierChangesRuntimeMetadata` | pass / pass |
| Text/plain consumer, producer, custom result operator | [Operator](https://caicloud.github.io/nirvana/zh-hans/concepts/operator.html) | Nirvana body consumer/producer and result operator | text endpoint; `TestTextPlainConsumerProducerAndResultOperatorBothArms` | pass / pass |
| Real Nirvana asynchronous job transition | [API guide](https://caicloud.github.io/nirvana/zh-hans/guides/api.html) | `definition.AsyncCreate`, job handlers | async-create/read/complete endpoints; `TestAsyncCreateJobStateTransitionOriginalAndBuild` | queued→completed pass / pass |
| Standard Huma typed health and version routes | [API guide](https://caicloud.github.io/nirvana/zh-hans/guides/api.html) | Huma `huma.Register`, adapter `Handle`, `humago.New`, group middleware | `/native/health`, `/native/version`; `TestTypedOperationAndMiddlewareMatchHumagoNativeAndNirvanaComposed`, `TestDynamicPortHTTPCutoverAndTypedHumaSmoke` | independent Huma baseline and composed Nirvana router both pass; paired status/header/body/middleware; legacy route retained |
| Synthetic native async status | [API guide](https://caicloud.github.io/nirvana/zh-hans/guides/api.html) | Huma native operation | `/native/async/{id}`; `TestNativeEnhancementIsAdditiveAndDocumented` | no Original equivalent / fixture only; not async acceptance |
| Unmigrated original endpoint retained | [API guide](https://caicloud.github.io/nirvana/zh-hans/guides/api.html) | original descriptor with `definition.Get` | `/legacy/status`; auth/rejection test verifies it remains callable | pass / pass (registered through `RegisterLegacy`) |

## Current gaps

The feature table records demonstrated features. In composed mode Huma typed binding/validation/response serialization run after Nirvana route/media admission, while legacy descriptors retain their original Nirvana execution chain. Native health/version endpoints are Huma operations, not Nirvana plugins. `/native/async/{id}` is a static fixture; the separate Nirvana job flow provides the actual queued-to-completed transition. Empty-body parity is not claimed: parity tests send explicit JSON body/content type for bodyless DELETE and completion operations. Response panic/committed-write boundaries and external telemetry are outside this demo. Cutover has no hot update or production gateway role, and the two in-memory stores are independent.

The adapter's original-vs-Build tests use the same handler functions and compare response/status/content type while asserting one write per arm. Rejection tests assert no extra write or handler replay. The upload test asserts one in-memory attachment per arm. `TestBehaviorComparisonOriginalVersusAdaptedFullScript` runs one 43-step script over every legacy route on two fresh apps and requires identical status, Content-Type, selected headers, normalized full body (error bodies included), trace and side-effect counts; only `deadline` and the process-global prefab `sequence` are normalized. `TestBehaviorComparisonDetectsDifference` checks the comparison is not vacuous. Each test creates a fresh app, preventing cross-test writes.
