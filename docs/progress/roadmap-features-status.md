# Roadmapa produktu i katalog feature'ów — stan realizacji

> Wersja dokumentu: 2026-11-18, stan `main` w commicie `4578233`.
> Źródło: PDF „Agent Execution Control Plane — Roadmap produktu i szczegółowy
> katalog feature'ów” (fazy 0–6, 65 feature'ów) oraz jego repozytoryjny
> odpowiednik `docs/reference/full-roadmap-features.md`.
> Ten dokument jest **migawką postępu**, a nie kontraktem wykonawczym.
> Kontraktem pozostają specy w `docs/features/` i ADR-y w `docs/adr/`.
> Szczegóły metody i ustalenia: `docs/audits/2026-11-18-spec-vs-repository.md`.

## Legenda

| Znak | Znaczenie |
| --- | --- |
| ✅ | Zrobione: kryteria akceptacji z PDF pokryte kodem i testami (z ewentualnymi świadomymi zawężeniami opisanymi w specu/ADR) |
| 🟡 | Częściowo: działa rdzeń albo część zakresu, brakuje wyraźnie nazwanych elementów |
| 📝 | Tylko dokumentacja: spec, ADR lub plan implementacji istnieją, kodu funkcji brak |
| ⬜ | Nie rozpoczęte: brak specu i kodu |

Weryfikacja wykonana 2026-11-18: `make check` — **OK** (wszystkie pakiety Go),
`desktop: npm run check` — **OK** (32 pliki testów, 333 testy, build, Storybook,
skan bundla). Testy e2e Electron (`make desktop-e2e`) nie były uruchamiane w tym
przeglądzie; ich wynik jest przywołany z dokumentacji (`docs/validation/`).

## 1. Podsumowanie liczbowe

| Faza | Wydanie | Feature'y | ✅ | 🟡 | 📝 | ⬜ | Exit criteria fazy |
| --- | --- | ---: | ---: | ---: | ---: | ---: | --- |
| 0 | v0.1 Execution Core | 7 | 7 | 0 | 0 | 0 | **Spełnione** (test integracyjny CLI) |
| 1 | v0.2 Agent Loop | 8 | 8 | 0 | 0 | 0 | **Spełnione** (demo z prawdziwym Codexem 2026-11-17) |
| 2 | v0.3 Local IDE | 12 | 4 | 8 | 0 | 0 | **Częściowo** — przepływ działa bez terminala, Run Inspector niekompletny |
| 3 | v0.4 Local Production Ready | 11 | 1 | 4 | 3 | 3 | **Niespełnione** — brak resume, timeoutów, artifact store, sekretów, polityk |
| 4 | v0.5 Connected Control Plane | 10 | 0 | 0 | 0 | 10 | Nie rozpoczęte |
| 5 | v1.0 Pro / Team | 12 | 0 | 0 | 0 | 12 | Nie rozpoczęte |
| 6 | Post-v1 | 5 | 0 | 0 | 0 | 5 | Nie rozpoczęte |
| **Razem** | | **65** | **20** | **12** | **3** | **30** | |

Poza katalogiem PDF repo zawiera dodatkowe pozycje: F1.6a (pętla korekcyjna dla
Shell node), F2.5a (redesign desktopu, PL/EN), F2.3-E6 (opcjonalny zapis wartości
runa) oraz drafty REL1/REL2 (release engineering, publiczny feed roadmapy).

## 2. Faza 0 — Execution Core (v0.1) — ZAKOŃCZONA

| ID | Feature | Prio | Status | Dowód w repo | Uwagi / odstępstwa od PDF |
| --- | --- | --- | --- | --- | --- |
| F0.1 | Graph Definition Schema v0 | P0 | ✅ | `internal/domain/graph`, `internal/adapter/yamlgraph`, ADR-0001 | YAML jest formatem źródłowym; plik JSON przechodzi walidację, bo parser YAML go akceptuje (sprawdzone ręcznie), ale nie ma osobnego testu „JSON równoważny”. **Timeout policy per node nie istnieje w schemacie** — przeniesione do F3.4 |
| F0.2 | Graph Validator i lint | P0 | ✅ | `agentplane graph validate [--json]`, ADR-0002 | Kody diagnostyk z sugestią naprawy, ostrzeżenie o nieużywanym outpucie |
| F0.3 | Data bindings na edge'ach | P0 | ✅ | ADR-0003, ADR-0008 | Typy: string, number, boolean, json, artifact_ref (ten ostatni jest tylko nieprzezroczystą referencją — brak artifact store, F3.5) |
| F0.4 | DAG Scheduler in-memory | P0 | ✅ | `internal/runtime/scheduler.go`, ADR-0004 | Stany pending/ready/running/succeeded/failed/skipped/cancelled; `--max-concurrency` |
| F0.5 | Shell Node | P0 | ✅ | `internal/shell`, ADR-0005 | Argv, cwd, allowlista env, stdout/stderr w `log_chunk` (porcje 16 KiB). Przy przerwaniu ubijany jest **bezpośredni proces, nie całe drzewo** (Codex ma grupę procesów) — domknięcie w F3.4. Pełny log jako artifact: brak (F3.5) |
| F0.6 | Event Model i Run/Node IDs | P0 | ✅ | `internal/domain/events`, ADR-0006 | UUID, sekwencja monotoniczna, `graph_version` (hash semantyczny `sha256-v0`), redakcja `--redact-env`. Schemat eventów obecnie w wersji 11 |
| F0.7 | CLI Runner | P0 | ✅ | `cmd/agentplane`, ADR-0007 | `run`, `graph validate`, `--input`, `--watch`, `--json`, kody wyjścia |

**Exit criteria:** spełnione — `cmd/agentplane/integration_test.go` dowodzi
nakładania się dwóch gałęzi barierą, przekazania danych i deterministycznego wyniku.

## 3. Faza 1 — Agent Loop (v0.2) — ZAKOŃCZONA

| ID | Feature | Prio | Status | Dowód w repo | Uwagi / odstępstwa od PDF |
| --- | --- | --- | --- | --- | --- |
| F1.1 | Agent Adapter Contract | P0 | ✅ | `internal/agent`, `internal/agent/adaptertest`, ADR-0009 | Mock przechodzi testy kontraktowe; runtime nie zawiera kodu providera. `prepare` i osobne `cancel` zastąpione kontekstem i detekcją (decyzja ADR-0009) |
| F1.2 | Pierwszy coding-agent adapter | P0 | ✅ | `internal/agent/codex`, ADR-0010, `docs/validation/F1.2-codex-smoke.md` | Codex CLI; detekcja wersji; zmienione pliki z Gita; anulowanie ubija grupę procesów. Drugi adapter (Claude Code, Gemini) — nie ma; CLI są tylko *wykrywane* w F2.11 |
| F1.3 | Unified Streaming | P0 | ✅ | `text_delta`, `tool_call_*`, `command_*`, `file_changed`, `status`, `progress_omitted`; ADR-0011 | Ograniczona pamięć i licznik pominięć zamiast backpressure blokującego scheduler |
| F1.4 | Gate Node | P0 | ✅ | `internal/gate`, ADR-0012 | Rodzaje: `command` (exit code), `json_predicate`, `reported_file_scope`, `changed_files_scope`. Gate jest tylko do odczytu |
| F1.5 | Check Result Contract | P0 | ✅ | `gate_check`, ADR-0013 | verdict, reason, evidence[] z typowanymi referencjami (file/test/diff/artifact), scope, check_id, duration. Status specu wciąż mówi „pending independent review” |
| F1.6 | Correction Loop + maxAttempts | P0 | ✅ | `internal/runtime/loop.go`, ADR-0014 | Domyślnie 3 próby, osobne attempt id, wynik `needs_intervention` po wyczerpaniu prób |
| F1.7 | Return Envelope / scoped retry | P0 | ✅ | `return_envelope`, ADR-0015, `docs/validation/F1.7-real-codex-demo.md` | Tylko **statyczne unity** (jeden node = jeden unit); dynamiczny `foreach`/fan-out z PDF architektury nie istnieje. Demo A/B/C/D z prawdziwym Codexem przeszło 2026-11-17 |
| F1.8 | Changed-files Scope Check | P1 | ✅ | ADR-0016 | Tryby warn/deny, evidence z plikami poza scope. Wykrywa naruszenie po fakcie, nie blokuje zapisu |

**Exit criteria:** spełnione — demo F1.7 (Codex 0.169.0): `z_c` dostaje `red`,
potem `green`; A, B, D nie są uruchamiane ponownie.

## 4. Faza 2 — Local IDE (v0.3) — W TOKU

| ID | Feature | Prio | Status | Co jest | Czego brakuje względem PDF |
| --- | --- | --- | --- | --- | --- |
| F2.1 | Graph Canvas | P0 | ✅ | Edytor na React Flow: zoom/pan, drag, porty, łączenie tylko zgodnych portów (`can_connect`), zapis do pliku przez operacje (ADR-0020/0021), layout jako metadane (ADR-0022) — przesunięcie nie zmienia hasha. Test wydajności 200 node'ów | Auto-layout i grupy (P1 w PDF) |
| F2.2 | Node Inspector | P0 | ✅ | Sekcje: inputs, execution, loop/checks, outputs, advanced; walidacja inline; pola z deskryptorów walidatorów (`internal/fields`); podgląd „resolved input” jako zadeklarowane źródło | Sekcja **permissions** (zależy od F3.9); podgląd rzeczywistych wartości wejścia przed runem |
| F2.3 | Edge/Data Mapping Editor | P0 | 🟡 | Łączenie portów na canvasie, usuwanie edge'y, operacje silnika dla graph inputs/bindings/outputs (E5a.1), opcjonalny zapis wartości runa (E6), ostrzeżenie o nieużywanym outpucie z walidatora, `IndependentNodes` w inspektorze | **E5a.2** panel bindingów, **E5a.3** edycja graph inputs/outputs i podpowiedzi równoległości, **E5b.1–E5b.2** inspektor edge'a z wartością, która przez niego przeszła. Spec gotowy, kod UI nie rozpoczęty |
| F2.4 | Graph Validation UI | P0 | ✅ | Panel Issues z severity i filtrem, klik → fokus node/edge, badge na elemencie, Run zablokowany tylko przez błędy, kolumna błędu parsowania | — |
| F2.5 | Live Execution Map | P0 | ✅ | Statusy waiting/ready/running/retrying/green/red/human_waiting, animowany edge przepływu, licznik prób i czas, stan z event streamu, odtworzenie po odświeżeniu | — |
| F2.6 | Run Inspector — Overview | P0 | 🟡 | Status runa, KPI: wall clock, czas node'ów, parallelism saved, retries; timeline node'ów; log eventów z filtrami Failed/Retried/Human; klik node'a bez utraty kontekstu | Spec luki gotowy (2026-11-17), niezaimplementowany: attempty na timeline, klikalne wiersze, **osobny czas agenta**, **gdzie system czekał**, **critical path**, lista porażek, filtr **slow**, identyfikacja runa (ID, start, wersja grafu) |
| F2.7 | Run Inspector — Node Detail | P0 | 🟡 | Selektor attemptów, check result z reason/evidence (także dla samodzielnego Gate po poprawce #99), decyzja human, log per attempt (limit 64 KiB) | Spec luki gotowy, niezaimplementowany: **resolved input / output**, artifacts, znormalizowane eventy agenta (aktywność), changed files poza widokiem porównania, adapter i wersja, czasy, podsumowanie wejścia próby, znaczniki obcięcia |
| F2.8 | Attempt Compare | P1 | 🟡 | F2.8a: porównanie verdict/reason/evidence/duration/changed files, badge „scope expanded”, dostępne z node detail | F2.8b: **diff między attemptami** — ADR-0026 proposed, plan implementacji gotowy, kod brak |
| F2.9 | Local Run History + SQLite | P0 | 🟡 | Historia w JSONL (`events.jsonl` + `graph.yaml` per run), `runs list/show`, ekran Runs, przetrwanie restartu i odtworzenie inspectora (e2e) | **Świadome odstępstwo: brak SQLite** (ADR-0019, ADR-0027). Brak listy per repo/graph z filtrami, brak branch/commit w metadanych, retencja ma pola w `store.json`, ale bez `runs prune` (F3.2) |
| F2.10 | Human Gate UI | P0 | 🟡 | Node `human`, stan human_waiting, ekran Approvals z licznikiem, approve/reject z komentarzem, event `approval_decided`, CLI `--approve/--reject`. Kryteria akceptacji PDF spełnione | Panel bez **blast radius**, listy changed files i linku do diffu; reject zawsze kończy run (ścieżka alternatywna odłożona) |
| F2.11 | Onboarding + Capability Detection | P1 | 🟡 | Raport capability (Git, Codex, wykrywanie innych agent CLI), ekran Welcome, API `/capabilities`; Linux zweryfikowany | Docker, lista repo, self-check runnera — odłożone do osobnego specu; brak weryfikacji natywnej na macOS/Windows; ADR-0025 nadal proposed |
| F2.12 | Graph Templates | P1 | 🟡 | `.agents/templates` (4 szablony + `index.json`), picker „New graph”, kopiowanie jako zwykły plik, wymagane capabilities | Zestaw inny niż w PDF: jest parallel-shell, shell-check-loop, human-approval, agent-scoped-approval; **brak** fix issue, code review, dependency update, repo audit |

**Exit criteria fazy 2** („projektuje graph, uruchamia, obserwuje live, analizuje
reason/evidence, obsługuje human gate i wraca do starszego runa bez terminala”):
**częściowo spełnione.** Cały przepływ jest możliwy w desktopie (edytor →
New run → live map → Approvals → Runs). Braki: Run Inspector nie pokazuje
wartości wejścia/wyjścia ani aktywności agenta, nie ma edytora bindingów, nie ma
anulowania runa z UI (brak endpointu `cancel` w API).

## 5. Faza 3 — Local Production Ready (v0.4) — ROZPOCZĘTA

| ID | Feature | Prio | Status | Co jest | Czego brakuje |
| --- | --- | --- | --- | --- | --- |
| F3.1 | Workspace Isolation / Git Worktrees | P0 | ✅ | `isolation: worktree` dla Agent node w pętli: worktree per node run, jawny base commit, integracja zaakceptowanych zmian na ref runa, retain never/failed/always, strażnik `source_workspace_changed`; ADR-0024, demo z Codexem | Opt-in; tylko Agent (nie Shell/Gate); downstream nie widzi plików upstream; integracja do ref runa, nie do brancha użytkownika; Linux/macOS; to nie sandbox |
| F3.2 | Durable Run State / Append-only Event Store | P0 | 🟡 | Zadania planu 1–4/9: zatrzymanie przy błędzie zapisu, trwałe tworzenie, odporny reader, `store.json`, lock runa i stan pochodny `running`/`interrupted`; ADR-0027 proposed (JSONL, bez SQLite) | Testy crash (kill w trakcie), snapshot podsumowania, retencja i `runs prune`, ekspozycja w API/desktop |
| F3.3 | Resume / Recovery | P0 | ⬜ | Wykrywanie runa przerwanego (F3.2) | Brak specu i kodu: plan recovery, decyzja użytkownika dla side effects, ponowne użycie accepted outputs |
| F3.4 | Cancellation i Timeouts | P0 | 📝 | Spec F3.4 + F3.4a (Windows), ADR-0028 proposed, plan. Istnieje: Ctrl-C/SIGTERM anuluje run, stan `cancelled`, grupa procesów Codexa | Timeout per node i per graph, graceful → hard kill, ubijanie drzewa procesów Shell, reason timeoutu, cancel z API/UI, Windows Job Object |
| F3.5 | Artifact Store Local | P0 | ⬜ | Typ wartości `artifact_ref`, typ evidence `artifact` | Brak specu i kodu |
| F3.6 | Graph Versioning i Run Reproducibility | P0 | 🟡 | Hash semantyczny (`graph_version`) w każdym evencie, snapshot `graph.yaml` przy każdym zapisanym runie, różne grafy → różne wersje | Brak specu; brak commita/brancha repo i wersji runtime w metadanych runa; brak wersji promptu/konfiguracji per node |
| F3.7 | Export JSON/CSV | P0 | ⬜ | `runs show --json`, surowe `events.jsonl` | Brak stabilnego schematu eksportu, CSV attemptów, manifestu artifactów |
| F3.8 | Secrets References | P0 | 📝 | Spec, ADR-0029 proposed, plan. Istnieje: `--redact-env` i redakcja logów | `secret_ref` w grafie, provider env/keychain, preflight braku sekretu |
| F3.9 | Permissions / Execution Policy v0 | P0 | 📝 | Spec, ADR-0029 proposed, plan, spike'i Landlock / user namespaces / Job Object w `experiments/spikes` | Allowed paths w trybie enforce, network/shell on/off, allowed commands, preflight i UI uprawnień |
| F3.10 | Cost/Usage Metadata | P1 | 🟡 | Kontrakt `UsageMetadata` z rozróżnieniem unknown vs 0; UI pokazuje tokeny jako unknown | Codex nie raportuje usage; brak agregacji i eksportu |
| F3.11 | Local Diagnostics | P1 | 🟡 | `agentplane version`, raport capability | Diagnostic bundle z redakcją, self-test, health DB/store |

**Exit criteria fazy 3:** niespełnione. Brakuje resume po restarcie, timeoutów,
trwałych artifactów oraz modelu sekretów i uprawnień.

## 6. Faza 4 — Connected Control Plane (v0.5) — NIE ROZPOCZĘTA

F4.1 Account + Authentication, F4.2 Projects, F4.3 Runner Enrollment, F4.4
Outbound Secure Connection, F4.5 Runner Presence & Health, F4.6 Remote Run
Start, F4.7 Cloud Run Metadata Sync, F4.8 Web Human Approval, F4.9 Cloud
Security Baseline, F4.10 Optional Artifact Sync — wszystkie ⬜.

Zgodnie z `AGENTS.md` cloud jest poza zakresem do jawnej aktywacji. W desktopie
pozycje „Projects” i „Runners” są w menu, ale wyszarzone („wkrótce”). Istniejące
elementy, na których faza 4 może się oprzeć: loopback API z tokenem (F2.5),
raport capability (F2.11), event stream SSE, decyzje human przez API.

## 7. Faza 5 — Pro / Team (v1.0) — NIE ROZPOCZĘTA

F5.1 Plans + Billing, F5.2 Scheduling, F5.3 Alerts / Notifications, F5.4
Long-term History & Analytics, F5.5 Organizations / Team Workspaces, F5.6 RBAC,
F5.7 Shared Graph Registry, F5.8 Shared Constraints / Learning Edge v1, F5.9
Policy Engine v1, F5.10 Audit Log, F5.11 Budgets & Guardrails, F5.12 Git
Provider Integration — wszystkie ⬜.

## 8. Faza 6 — Post-v1 — NIE ROZPOCZĘTA

F6.1 Managed Ephemeral Runners, F6.2 Managed Artifact Storage, F6.3 Enterprise
Identity, F6.4 Advanced Policy / Approval Chains, F6.5 Plugin/Adapter SDK —
wszystkie ⬜.

## 9. Ekrany produktu (sekcja 6 PDF)

| Surface | Faza w PDF | Stan | Co jest w repo |
| --- | --- | --- | --- |
| CLI run/validate | 0 | ✅ | `agentplane run`, `graph validate`, `runs list/show`, `serve`, `version`; `--watch`, `--json`, kody wyjścia |
| Graph Canvas | 2 | ✅ | Ekran Editor: canvas, YAML obok (CodeMirror), panel Issues, picker szablonów |
| Node Inspector | 2 | ✅ (bez permissions) | Panel inspektora w edytorze |
| Run Inspector | 2 | 🟡 | Ekran Run: mapa live, KPI, timeline, log eventów, panel node'a, porównanie attemptów |
| Local History | 2 | 🟡 | Ekran Runs (lista, status, start); bez filtrów i metadanych repo |
| Human Approval | 2 local / 4 web | 🟡 lokalnie, ⬜ web | Ekran Approvals + ApprovalBox w panelu node'a |
| Runner Fleet | 4 | ⬜ | Pozycja „Runners” w menu wyszarzona |
| Analytics | 5 | ⬜ | — |
| Organization/Admin | 5 | ⬜ | Pozycja „Projects” wyszarzona |

Dodatkowo, poza listą PDF: Dashboard, New run (wybór grafu przez grant pliku,
walidacja, start), Welcome (onboarding), Settings (m.in. przełącznik zapisu
wartości runa), przełącznik języka PL/EN, Storybook dev-only.

## 10. Backlog pod pracę licencjacką (sekcja 7 PDF)

| Zakres rekomendowany | Stan |
| --- | --- |
| F0.1–F0.7 | ✅ komplet |
| F1.1–F1.7 | ✅ komplet (plus F1.8) |
| F2.5–F2.10 | F2.5 ✅; F2.6, F2.7, F2.8, F2.9, F2.10 🟡 |
| F3.1–F3.9 | F3.1 ✅; F3.2, F3.6 🟡; F3.4, F3.8, F3.9 📝; F3.3, F3.5, F3.7 ⬜ |
| opcjonalnie F4.3–F4.8 | ⬜ |

Najbliższe luki blokujące tezę: **F3.3 Resume** (Demo D), **F3.7 Export
JSON/CSV** (analiza eksperymentów), **F3.4 Timeouts**, domknięcie **F2.6/F2.7**.
`experiments/run-log.csv` ma tylko nagłówek — dane eksperymentów A/B/C nie są
jeszcze zbierane.

## 11. Metryki (sekcja 8 PDF)

| Metryka | Faza | Czy da się dziś zmierzyć |
| --- | --- | --- |
| Run success rate | 1 | Tak, z `run_finished` w historii (brak agregacji) |
| Time-to-green | 1 | Tak, z eventów attemptów i `gate_check` (brak gotowego wyliczenia) |
| Attempts per accepted unit | 1 | Tak, z `return_envelope` / attemptów |
| Local retry ratio | 1 | Tak, z attemptów per node |
| Parallelism saved | 2 | Tak, KPI w Run Inspectorze |
| Human intervention rate | 2 | Tak, z `approval_*` (brak agregacji) |
| Recovery success | 3 | Nie (brak F3.3) |
| Policy violation rate | 3 | Częściowo: naruszenia `changed_files_scope`; brak F3.9 |
| Remote start reliability, Approval latency | 4 | Nie |
| Cost per accepted result | 3/5 | Nie (Codex nie raportuje usage) |
| Rollback/manual override rate | 5 | Nie |

## 12. Milestones demonstracyjne (sekcja 12 PDF)

| Demo | Stan | Dowód / brak |
| --- | --- | --- |
| A — „Graph naprawdę ma znaczenie” | ✅ | Test integracyjny CLI, KPI „parallelism saved” w desktopie |
| B — „Loop naprawia unit, nie batch” | ✅ | `docs/validation/F1.7-real-codex-demo.md` (Codex, 2026-11-17) |
| C — „Debugger zamiast terminali” | 🟡 | `docs/validation/F2.5-desktop-demo.md`; brak input/output i diffu w Run Inspectorze |
| D — „Produkcyjność lokalna” | ⬜ | Wymaga F3.2 (w toku) i F3.3 (brak) |
| E — „Cloud nie musi mieć repo” | ⬜ | Faza 4 |

## 13. Kryterium gotowości v1.0 (sekcja 13 PDF)

| # | Warunek | Stan |
| --- | --- | --- |
| 1 | Graph execution stabilne, wersjonowane, wznawialne | 🟡 stabilne i wersjonowane, **niewznawialne** |
| 2 | Gate/loop/retry z jawną, testowalną semantyką | ✅ |
| 3 | Run Inspector wyjaśnia każdy red verdict i decyzję człowieka | 🟡 reason/evidence tak; input/output i diff nie |
| 4 | Local Free użyteczny bez konta cloud | ✅ |
| 5 | Remote control przez outbound runner connection | ⬜ |
| 6 | Team: RBAC i audit | ⬜ |
| 7 | Niezaakceptowany attempt nie przecieka | 🟡 wartości runtime — tak (F1.7); pliki — tylko z `isolation: worktree` dla Agent |
| 8 | Bezpieczne defaulty sekretów i privacy | 🟡 redakcja env, loopback, opt-in zapis wartości; brak `secret_ref` |
| 9 | Pomiar time-to-green, retry rate, human rate | 🟡 dane są w eventach, brak eksportu i agregacji |
| 10 | Managed compute opcjonalne | ✅ (nie istnieje, nic od niego nie zależy) |

## 14. Rekomendowana kolejność dalszych prac

1. Domknąć **F3.2** (zadania 5–9: crash testy, snapshot, retencja, API/desktop).
2. Zaimplementować **F3.4** według gotowego planu (timeouty, drzewo procesów,
   cancel z API i przycisk Cancel w desktopie).
3. Napisać spec i ADR **F3.3 Resume/Recovery** — to warunek Demo D i v1.0 (1).
4. Napisać spec **F3.7 Export JSON/CSV** i wpiąć `experiments/` — to warunek
   części eksperymentalnej pracy.
5. Zaimplementować luki **F2.6/F2.7** (specy gotowe) oraz **F2.3 E5a.2–E5b.2**.
6. **F3.8/F3.9** według wspólnego planu; potem **F3.5** i **F3.6** (spec).
7. Fazę 4 aktywować dopiero po exit criteria fazy 3.
