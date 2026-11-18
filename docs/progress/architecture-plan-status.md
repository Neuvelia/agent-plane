# Architektura produktu i plan MVP — stan realizacji

> Wersja dokumentu: 2026-11-18, stan `main` w commicie `4578233`.
> Źródło: PDF „Agent Execution Control Plane — Architektura produktu, plan MVP
> i wykorzystanie w pracy licencjackiej”, wersja 0.1 z 11.09.2026, oraz jego
> repozytoryjny odpowiednik `docs/reference/full-architecture-plan.md`.
> Dokument zestawia każdą sekcję planu z tym, co faktycznie jest w repo.
> Status feature'ów (F0.1–F6.5): `docs/progress/roadmap-features-status.md`.

Legenda: ✅ zrobione · 🟡 częściowo · 📝 tylko spec/ADR · ⬜ nie rozpoczęte ·
↔ świadome odstępstwo od planu (z decyzją w ADR lub specu).

## 1. Streszczenie stanu

Zbudowany jest **headless execution engine w Go** z CLI, loopback API i
**desktopem Electron/React**. Zaimplementowane są: schemat grafu, walidator,
scheduler DAG, cztery typy node'ów z MVP (Agent, Shell, Gate, Human), pętla
korekcyjna z limitem prób, return envelope ze scoped retry, adapter Codex,
streaming, lokalna historia runów (JSONL), izolacja worktree (opt-in) oraz
edytor grafu, mapa live, Run Inspector (częściowo) i zatwierdzanie human gate.

Nie ma: resume/recovery, timeoutów, artifact store, eksportu JSON/CSV,
sekretów jako referencji, polityk wykonania oraz całej warstwy cloud.

## 2. Zasady projektowe (sekcja 3 PDF)

| Zasada | Stan | Jak jest egzekwowana |
| --- | --- | --- |
| 3.1 Local-first | ✅ | Brak zależności sieciowych w silniku; API tylko na loopback z tokenem; nic nie wymaga konta |
| 3.2 Graph jako kontrakt | ✅ | Plik YAML w Git uruchamiany z CLI; edytor zapisuje do tego samego pliku przez wersjonowane operacje (ADR-0020/0021) |
| 3.3 Node ≠ agent | ✅ | Shell, Gate i Human są równorzędne; Gate jest deterministyczny |
| 3.4 Edge = zależność danych | ✅ | Każdy edge wiąże output z inputem; brak edge'y „tylko kolejność” |
| 3.5 Deterministyczna walidacja przed modelem | ✅ | Gate: exit code, predykat JSON, scope plików; brak LLM-as-judge |
| 3.6 Retry najmniejszej jednostki | ✅ | F1.7, potwierdzone demem z Codexem |
| 3.7 Human-in-the-loop wg blast radius | 🟡 | Node `human` jest jawnie umieszczany w grafie; **brak klasyfikacji blast radius** (L1–L3) i warunku `when` |
| 3.8 Pełny trace | ✅ | Event schema v11; każda tranzycja ma event; `graph_version` w każdym evencie |

## 3. Architektura wysokiego poziomu (sekcja 4 PDF)

| Warstwa | Stan | Realizacja w repo |
| --- | --- | --- |
| UI / IDE | 🟡 | `desktop/` — Electron + React 19 + React Flow + CodeMirror; tylko obserwuje i steruje przez API |
| Execution Engine | ✅ | `internal/runtime`, `internal/domain/*` |
| Local Runner | 🟡 | Brak osobnego daemona: `agentplane serve` działa jako sidecar desktopu, `agentplane run` jako proces CLI |
| Adapters | 🟡 | Shell i Codex; brak generic ProcessAdapter jako osobnego bytu, brak drugiego coding-agenta |
| Cloud Control Plane | ⬜ | Poza zakresem do aktywacji fazy 4 |

**Tryby wykonania (4.1):** Local ✅; Self-hosted runner ⬜; Managed cloud ⬜.

**Układ repo vs sekcja 10 PDF roadmapy (`packages/…`):** ↔ repo stosuje układ Go
`cmd/` + `internal/` zgodny z clean architecture (domain → application →
adapters), plus `desktop/`. Mapowanie: graph-schema → `internal/domain/graph` +
`internal/adapter/yamlgraph`; execution-core → `internal/runtime`; event-model →
`internal/domain/events`; adapters → `internal/shell`, `internal/agent`,
`internal/gate`; workspace-manager → `internal/worktree`, `internal/gitstate`;
local-store → `internal/store` (JSONL, nie SQLite); runner-daemon → `serve` +
`internal/api`; cli → `cmd/agentplane`; ui → `desktop/`; cloud-api — brak.

## 4. Model wykonania (sekcja 5 PDF)

| Element | Stan | Uwagi |
| --- | --- | --- |
| 5.1 Graph: jednostki, równoległość, zależności, fan-in | ✅ | |
| 5.1 Fan-out (`foreach`) | ⬜ ↔ | F1.7 świadomie wspiera tylko statyczne unity; brak dynamicznego fan-outu |
| 5.1 Powrót do wcześniejszego kroku | ⬜ | Graf jest DAG-iem; korekta tylko wewnątrz node'a |
| 5.2 Typy MVP: Agent, Shell, Gate, Human | ✅ | |
| 5.2 Splitter, Code, Merge, Router, Artifact transform, Subgraph, External trigger | ⬜ | Zgodnie z planem — „kolejne wersje” |
| 5.3 Kontrakt node'a (input → output) | ✅ | Jawne porty z typami; output Agent: summary, changed files, usage (opcjonalnie) |
| 5.4 Edge jako przepływ danych + podpowiedź równoległości | 🟡 | Edge'e — tak; `IndependentNodes` w inspektorze; panel podpowiedzi F2.3 E5a.3 — nie |
| 5.5 Loop: check, limit, evidence per próba, scope, eskalacja | ✅ | Agent (F1.6) i Shell (F1.6a); eskalacja = `needs_intervention` |
| 5.6 Return envelope (unit, verdict, reason, evidence, scope, attempt) | ✅ | Event `return_envelope` |
| 5.7 Gate z wyników deterministycznych | ✅ | |
| 5.7 Gate z trajektorii, historii jakości, polityki blast radius, oceny modelu | ⬜ | Świadomie odłożone |

### Przykładowy format grafu (sekcja 11 PDF)

↔ Składnia w repo różni się od przykładu w PDF: zamiast `depends_on`, `foreach`,
`{{ nodes.x.output }}` i `when` są jawne `edges` (from output → to input) i
`graph_input_bindings`. Wynika to z zasady „edge = zależność danych” (ADR-0001,
ADR-0003). Wymagania 11.1 (deterministyczny parser, walidacja schematu, jawne
referencje, `schema_version`, diffowalność, brak sekretów, ścieżka migracji) —
✅, z zastrzeżeniem, że brak sekretów w pliku nie jest jeszcze wymuszany przez
`secret_ref` (F3.8).

## 5. Model stanów (sekcja 6 PDF)

| Plan | Repo | Stan |
| --- | --- | --- |
| Node: PENDING, READY, RUNNING, SUCCEEDED, FAILED, CANCELED | `pending`, `ready`, `running`, `succeeded`, `failed`, `cancelled` + `skipped` | ✅ |
| Node: CHECKING, CORRECTING | Fazy attemptu w eventach (`attempt_started`, `gate_check`, `attempt_input`); UI pokazuje `retrying` | ↔ |
| Node: WAITING_FOR_HUMAN | Node `running` + `approval_requested`; UI pokazuje `human_waiting` | ↔ |
| Run: RUNNING, SUCCEEDED, FAILED, CANCELED | `running`, `succeeded`, `failed`, `cancelled` | ✅ |
| Run: QUEUED | brak (run startuje od razu) | ⬜ |
| Run: WAITING_FOR_HUMAN | pochodny z eventów (licznik w Approvals) | ↔ |
| Run: INTERRUPTED | pochodny w store (lock + brak `run_finished`, F3.2 zadanie 4) | 🟡 |
| 6.3 Event sourcing / append-only log | `events.jsonl` per run; stan UI to redukcja eventów | ✅ (trwałość zapisu w toku, F3.2) |

Mapowanie przykładowych eventów z PDF: RunStarted → `run_started`; NodeReady →
`node_ready`; NodeStarted → `node_started`; AgentOutputChunk → `text_delta` /
`log_chunk`; **ArtifactCreated → brak** (F3.5); CheckStarted/CheckFailed →
`attempt_started` / `gate_check` (verdict red); CorrectionRequested →
`return_envelope` + `attempt_input`; NodeSucceeded → `node_finished`;
HumanApprovalRequested/HumanApproved → `approval_requested` / `approval_decided`;
RunSucceeded → `run_finished`.

## 6. Local Runner (sekcja 7 PDF)

| Odpowiedzialność | Stan | Uwagi |
| --- | --- | --- |
| Rejestracja workspace'ów | 🟡 | Granty plików grafu w desktopie (ADR-0021); brak rejestru repo |
| Uruchamianie agentów i procesów | ✅ | |
| Kontrola cwd, env, sekretów | 🟡 | cwd i allowlista env — tak; sekrety jako referencje — F3.8 📝 |
| Lifecycle procesów | 🟡 | Codex: grupa procesów; Shell: tylko proces bezpośredni (F3.4) |
| Timeout, CPU/memory | ⬜ | F3.4 📝; limity zasobów — brak |
| Streaming stdout/stderr i eventów | ✅ | SSE z API do desktopu |
| Zarządzanie artifactami | ⬜ | F3.5 |
| Wykonywanie checków | ✅ | |
| Utrzymywanie stanu runa | 🟡 | JSONL; trwałość i retencja w toku (F3.2) |
| Komunikacja z cloud | ⬜ | Faza 4 |

**Interfejsy (7.2):** ↔ binarka nazywa się `agentplane`, nie `agentctl` (REL1
planuje zmianę nazwy na `tarmind`).
`agentctl run` → `agentplane run` ✅; `graph validate` ✅; `runs inspect` →
`runs show` ✅; `runner status` ⬜ (raport capability jest tylko w API `/capabilities`).
Lokalne API HTTP na `127.0.0.1` ✅; live events przez **SSE** zamiast WebSocket ↔.

**Połączenie z chmurą (7.3):** ⬜.

## 7. Adaptery (sekcja 8 PDF)

| Element | Stan | Uwagi |
| --- | --- | --- |
| 8.1 Interfejs: id, detect, execute (strumień), cancel | ✅ | ADR-0009; anulowanie przez `context` |
| 8.2 ShellAdapter | ✅ | Jako Shell node |
| 8.2 Generic ProcessAdapter | 🟡 | Shell node pełni tę rolę; brak osobnego adaptera |
| 8.2 Jeden coding-agent adapter | ✅ | Codex CLI |
| 8.2 Drugi adapter (neutralność) | ⬜ | Mock w testach kontraktowych zastępuje go częściowo |
| Kolejne: Claude Code, Gemini CLI, Python, HTTP/MCP, Docker, custom | ⬜ | Claude Code, Cursor Agent, Gemini CLI są tylko **wykrywane** (F2.11) |
| 8.3 Capability detection | 🟡 | Git, Codex, agent CLI; brak Docker, Python, Node; graph nie deklaruje wymagań poza szablonami |

## 8. Repozytorium, workspace i izolacja (sekcja 9 PDF)

| Poziom izolacji | Stan |
| --- | --- |
| 1. Osobny git worktree | ✅ opt-in dla Agent node (F3.1) |
| 2. Osobny proces z ograniczonym env | ✅ allowlista env dla Shell i Codex |
| 3. Kontener | ⬜ |
| 4. Ephemeral VM / sandbox | ⬜ (spike'i Landlock/userns/Job Object w `experiments/spikes`) |

Tryb managed cloud (9.2) — ⬜.

## 9. Bezpieczeństwo i model zaufania (sekcja 10 PDF)

| Zasada | Stan |
| --- | --- |
| Sekrety po stronie execution environment | ✅ (nie ma cloud) |
| Cloud nie dostaje sekretów | ✅ (nie ma cloud) |
| Node deklaruje wymagane capabilities | 🟡 tylko szablony (`index.json`) |
| Least privilege | 🟡 allowlista env, Gate read-only; brak F3.9 |
| Read-only workspace | 🟡 Gate jest read-only; brak trybu dla Agent/Shell |
| Blokada komend lub ścieżek | 🟡 wykrywanie po fakcie (`changed_files_scope` deny); blokada przed zapisem — F3.9 📝 |
| Human approval: kto/co/kiedy | 🟡 co i kiedy — tak; kto — brak tożsamości (tryb lokalny) |
| Retencja logów/artifactów | 🟡 pola retencji w `store.json`, bez wykonania (F3.2) |
| Nie wysyłać kodu do cloud | ✅ |
| 10.2 Blast radius policy L1–L3 | ⬜ |
| 10.3 Policy engine | ⬜ (F5.9) |

Dodatkowo zrealizowano: sandboxowany renderer Electron (contextIsolation, CSP,
allowlista tras), granty plików grafu, redakcja env w eventach, opcjonalny i
ograniczony zapis wartości (ADR-0023), sprawdzanie zaufania przy zapisie grafu.

## 10. Run Inspector (sekcja 12 PDF)

### 12.1 Widok całości

| Element z przykładu | Stan |
| --- | --- |
| Drzewo node'ów ze statusem i czasem | ✅ mapa + timeline |
| Attempty pod node'em (RED → GREEN) z reason | 🟡 w panelu node'a i porównaniu; nie na timeline |
| WAITING FOR HUMAN | ✅ |
| Wall clock | ✅ |
| Agent time | 🟡 sumowany czas wszystkich node'ów, bez wydzielenia agentów |
| Parallelism saved | ✅ |
| Model calls | ⬜ |
| Retries | ✅ |

### 12.2 Widok node'a

| Element | Stan |
| --- | --- |
| Input | ⬜ (F2.7) |
| Resolved prompt/command | ↔ świadomie nie zapisywane (ochrona przed wyciekiem) |
| Adapter i wersja | 🟡 w eventach, nie w panelu |
| Stream wydarzeń | 🟡 surowy log tak; znormalizowane eventy agenta nie |
| Output | ⬜ (F2.7; wartości dostępne opt-in) |
| Artifacty | ⬜ (F3.5) |
| Changed files / diff metadata | 🟡 tylko w porównaniu attemptów; diff — F2.8b |
| Check command | ↔ świadomie ukryte (może zawierać wartości środowiska) |
| Verdict, evidence | ✅ |
| Retry history | ✅ |
| Scope kolejnej korekty | 🟡 w porównaniu (feedback), nie w podsumowaniu próby |
| Duration | 🟡 na mapie, nie w panelu |
| Token/usage | ⬜ (Codex nie raportuje) |

### 12.3 Debug operations

Replay node, rerun from node, rerun failed units only, clone run with changed
input, compare two graph versions, export run to JSON — **wszystkie ⬜**.
Compare two attempts — 🟡 (F2.8a, bez diffu treści).

## 11. Model domenowy i storage (sekcja 13 PDF)

| Encja | Stan |
| --- | --- |
| GraphDefinition, NodeDefinition, EdgeDefinition | ✅ |
| GraphVersion | 🟡 hash semantyczny + snapshot `graph.yaml`; brak osobnego bytu/rejestru |
| Run, NodeRun, Attempt, CheckResult, ExecutionEvent | ✅ |
| Approval | ✅ (lokalnie, bez tożsamości) |
| Workspace | 🟡 worktree per node run; brak rejestru workspace'ów |
| Artifact | ⬜ |
| Constraint | ⬜ (F5.8) |
| Runner | ⬜ (faza 4) |

**Storage (13.2):** ↔ zamiast SQLite — JSONL per run + `store.json` (ADR-0019,
ADR-0027 proposed: „JSONL, no SQLite”). Filesystem dla dużych artifactów — ⬜.
YAML dla grafów — ✅. Git dla wersjonowania grafów — ✅ (poza produktem).

## 12. Cloud Control Plane i model cenowy (sekcje 14–15 PDF)

⬜ w całości. Funkcje Pro (konto, remote, harmonogramy, alerty, web approvals,
analytics, backup) i Team (shared graphs, RBAC, audit, policies, shared runners,
Git integrations, budżety, SSO) nie są rozpoczęte. Kolumna „Local Free” jest
zrealizowana w większości: visual editor ✅, local runner ✅, loops/gates ✅,
local history ✅ (bez SQLite).

## 13. Stack techniczny (sekcja 16 PDF)

| Rekomendacja | Wybór w repo | Uwagi |
| --- | --- | --- |
| React + TypeScript | ✅ React 19 + TS | |
| React Flow | ✅ `@xyflow/react` 12 | |
| Monaco Editor | ↔ CodeMirror 6 | lżejszy, YAML |
| WebSocket/SSE | ✅ SSE | |
| Desktop: Tauri (rekomendacja) / Electron | ↔ Electron 45 | Plan dopuszczał Electron jako szybszy start |
| Runner: Go lub Rust | ✅ Go | single binary |
| Cloud: monolit, Postgres, object storage | ⬜ | |
| Design system | — | Tarmind design system, PL/EN, Storybook dev-only (F2.5a) |

## 14. MVP — zakres rekomendowany (sekcja 17 PDF)

### 17.1 Must-have

| # | Element | Stan |
| --- | --- | --- |
| 1 | Graph definition YAML/JSON | ✅ |
| 2 | Visual graph editor | ✅ (bez edytora bindingów F2.3) |
| 3 | Agent, Shell, Gate, Human | ✅ |
| 4 | DAG scheduler | ✅ |
| 5 | Równoległe wykonanie | ✅ |
| 6 | Local runner | ✅ CLI + `serve` |
| 7 | Prawdziwy coding-agent adapter | ✅ Codex |
| 8 | Loop z max attempts | ✅ |
| 9 | Retry tylko błędnej jednostki | ✅ |
| 10 | Run Inspector | 🟡 |
| 11 | Event log | ✅ |
| 12 | Resume/recovery w podstawowej formie | ⬜ |
| 13 | Export run → JSON | 🟡 `runs show --json` bez stabilnego schematu eksportu |
| 14 | Basic metrics: duration, attempts, status, wynik testu | ✅ |

**Wynik: 11 ✅, 2 🟡, 1 ⬜ z 14.**

### 17.2 Should-have

| Element | Stan |
| --- | --- |
| Git worktrees | ✅ |
| Compare attempts | 🟡 bez diffu |
| Graph validation | ✅ |
| Capability detection | 🟡 |
| Changed-files scope check | ✅ |
| Token/cost metadata | 🟡 kontrakt, brak danych |
| Reusable graph templates | 🟡 inny zestaw niż w roadmapie |

### 17.3 Poza MVP

Żaden element z listy „poza MVP” nie został zbudowany — zgodnie z planem.

## 15. Plan realizacji (sekcja 18 PDF)

| Faza | Kryterium ukończenia z planu | Stan |
| --- | --- | --- |
| 0 | Trzy Shell node'y, dwie gałęzie równolegle, złączenie wyników | ✅ |
| 1 | Agent zmienia repo, test, reason/evidence, poprawia tylko swój unit | ✅ |
| 2 | Typowy workflow projektowany i debugowany bez terminala | 🟡 |
| 3 | Worktrees, resume, cancel, timeouts, artifacts, export, wersjonowanie | 🟡 worktrees ✅, cancel częściowo, reszta brak |
| 4 | Account, runner, outbound connection, remote start, web approval | ⬜ |
| 5 | Scheduling, history, teams, RBAC, audit, billing | ⬜ |

## 16. Praca licencjacka (sekcja 19 PDF)

| Element | Stan |
| --- | --- |
| Mechanizmy z tezy inżynierskiej (19.1) | ✅ orkiestracja, obserwowalność, jawna walidacja, ograniczone retry; 🟡 izolacja, zarządzanie stanem, human-in-the-loop; ⬜ audyt z tożsamością, kontrola uprawnień |
| RQ1 (gate + pętle → success rate) | Aparatura gotowa (F1.4–F1.6); brak zebranych danych |
| RQ2 (równoległość → wall clock) | Aparatura gotowa (F0.4, KPI parallelism saved) |
| RQ3 (limit korekt → koszt/czas) | Częściowo: brak danych o koszcie (usage) |
| RQ4 (retry jednostki → mniej zmian) | Aparatura gotowa (F1.7, F1.8) |
| RQ5 (deterministyczne kryteria → interwencje) | Aparatura gotowa (F2.10 + eventy) |
| Warianty A/B/C/D (19.3) | Nie przygotowane jako zestaw zadań i grafów |
| Rekord powtarzalności (19.5) | 🟡 większość pól jest w eventach (run_id, graph_version, node, attempt, adapter, czasy, verdict, changed_files); brak `usage` i eksportu płaskiego |
| `experiments/run-log.csv` | Tylko nagłówek, brak danych |

## 17. Kryteria sukcesu MVP (sekcja 22 PDF)

| # | Krok scenariusza | Stan |
| --- | --- | --- |
| 1 | Użytkownik wskazuje lokalne repo | 🟡 przez pole `workspace` w grafie i grant pliku; brak wyboru repo w UI |
| 2 | Graph: planner, dwa równoległe workery, test gate, human gate | 🟡 możliwe ze statycznymi unitami; planner nie może dynamicznie tworzyć unitów |
| 3 | Niezależne jednostki równolegle | ✅ |
| 4 | Jeden worker zawodzi test | ✅ |
| 5 | Tylko jego unit wraca z reason, evidence, scope | ✅ |
| 6 | Druga próba przechodzi | ✅ |
| 7 | Brak ponownego wykonania poprawnych node'ów | ✅ |
| 8 | Operacja o wysokim blast radius zatrzymuje się na human gate | 🟡 human gate jest jawnym node'em, nie wynika z blast radius |
| 9 | Pełny trace i accept/reject | 🟡 accept/reject ✅; trace bez input/output i diffu w UI |
| 10 | Historia po restarcie | ✅ |
| 11 | Eksport JSON/CSV do analizy | 🟡 JSONL i `runs show --json`; brak CSV |

**Wynik: 6 ✅, 5 🟡, 0 ⬜.**

## 18. Kolejność decyzji projektowych (sekcja 23 PDF)

| # | Krok | Stan |
| --- | --- | --- |
| 1 | GraphDefinition, NodeDefinition, ExecutionEvent | ✅ |
| 2 | Headless engine z Shell node | ✅ |
| 3 | Gate i failed-unit retry | ✅ |
| 4 | Prawdziwy agent adapter | ✅ |
| 5 | Run Inspector na event logu | 🟡 |
| 6 | Visual graph editor | ✅ |
| 7 | Trwałość, worktrees, resume | 🟡 worktrees ✅, trwałość w toku, resume ⬜ |
| 8 | Pilotaż eksperymentów | ⬜ |
| 9 | Cloud control plane | ⬜ |
| 10 | Managed runners | ⬜ |

Kolejność została zachowana z jednym odstępstwem: edytor grafu (krok 6)
powstał przed domknięciem Run Inspectora (krok 5).

## 19. Ryzyka (sekcja 21 PDF) — jak się zmaterializowały

| Ryzyko | Ocena dziś |
| --- | --- |
| 21.1 Zbyt szeroki zakres | Kontrolowane: cloud nieruszony, ale równolegle powstało dużo pracy UI i drafty release (REL1/REL2) przed fazą 3 |
| 21.2 Lock-in do jednego agenta | Częściowo: jeden realny adapter (Codex); kontrakt neutralny i testowany mockiem |
| 21.3 Brak jednoznacznych checków | Zmitygowane: tylko deterministyczne Gate'y |
| 21.4 Bezpieczeństwo | Otwarte: brak polityk, sekretów jako referencji i sandboxa; agent może pisać poza scope (wykrywane po fakcie) |
| 21.5 Zbyt ciężki runner | Zmitygowane: single binary, bez bazy danych |
| 21.6 UI celem samym w sobie | Do pilnowania: edytor dojrzały, Run Inspector i recovery wciąż niekompletne |
