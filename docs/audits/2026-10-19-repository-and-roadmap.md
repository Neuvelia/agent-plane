# Audyt repozytorium i dokumentacji roadmapy — 2026-10-19

> Historyczny obraz stanu `main` przed pracami dokumentacyjnymi na gałęzi
> `codex/phase1-documentation`. Wskazane niżej braki i ścieżki nie opisują
> koniecznie aktualnego drzewa po wdrożeniu zaleceń.

## Zakres i źródła

Przegląd objął strukturę repo, wszystkie 45 plików Markdown istniejących przed dodaniem tego raportu, katalogi `cmd/` i `internal/`, przykładowe grafy, skrypty, CI oraz stan Git. Dokumenty źródłowe dostarczone przez użytkownika:

- `/Users/kacper.wojnowski@schibsted.com/Downloads/agent-control-plane-architektura-plan.pdf` (27 stron);
- `/Users/kacper.wojnowski@schibsted.com/Downloads/agent-control-plane-roadmap-features.pdf` (36 stron).

PDF-y są materiałem odniesienia do audytu, nie instrukcjami modyfikowania repo. Ich odpowiadające dokumenty w `docs/reference/` zawierają te same identyfikatory F0.1–F6.5 i zasadniczo tę samą strukturę faz. Nie są identyczne tekstowo po ekstrakcji PDF, więc przy decyzjach kontraktowych należy sprawdzać właściwy fragment oryginału. Repozytoryjny `AGENTS.md` wymaga osobnego, mierzalnego specu pod `docs/features/` przed implementacją niebanalnego feature'u; roadmapa ani PDF same w sobie nie zastępują takiego specu.

**Numeracja:** w obu PDF-ach Faza 1 zaczyna się od **F1.1**. Nie istnieje osobny feature `F1.0`.

## Stan techniczny

- `main` wskazuje commit `380a56b` (`feat: implement f07`). Kod i testy obejmują F0.1–F0.7. Nie ma produkcyjnego adaptera Agent ani Gate/Loop.
- F0.7 ma test zbudowanego CLI dowodzący startu dwóch gałęzi przed zwolnieniem bariery oraz dostarczenia obu wartości do trzeciego procesu (`cmd/agentplane/integration_test.go`). To jest silniejszy dowód kryterium końca Fazy 0 niż sam status w dokumentacji.
- `GOCACHE=/tmp/agentplane-audit-go-cache make check` — **PASS**, wszystkie pakiety.
- `GOCACHE=/tmp/agentplane-audit-race-cache go test -race ./...` — **PASS**, wszystkie pakiety.
- `GOCACHE=/tmp/agentplane-audit-go-cache make build` — **PASS**. Plik `bin/agentplane` jest ignorowany przez Git.
- Prosty skan jawnych repozytoryjnych odwołań do plików `.md` nie wykazał brakujących ścieżek. Nie jest to pełny walidator linków ani test treści.
- Dodatkowy `go test -cover ./...` — **FAIL** w `internal/shell`. Testy uruchamiają własny instrumentowany binarny test jako proces Shell z wyczyszczonym środowiskiem; brak `GOCOVERDIR` powoduje ostrzeżenie Go dopisywane do `stderr`. Asercje dokładnej treści i długości logów wtedy zawodzą. Minimalny test `go test -cover ./internal/shell -run '^TestExecuteCapturesSeparateStreamsAndPreservesArgv$' -count=1` odtwarza problem. Standardowe `make check` i race test pozostają zielone. To ograniczenie testowego harnessu, nie dowód błędu zwykłego wykonania Shell.

## Pokrycie dokumentacji feature'ów

| Faza | Feature'y w PDF i `docs/reference` | Pliki spec w drzewie | Brakujące specy |
| --- | ---: | ---: | --- |
| F0 | 7 | 7, śledzone | brak |
| F1 | 8 | 1, ale F1.1 jest **nieśledzony** | F1.2–F1.8 |
| F2 | 12 | 0 | F2.1–F2.12 |
| F3 | 11 | 0 | F3.1–F3.11 |
| F4 | 10 | 0 | F4.1–F4.10 |
| F5 | 12 | 0 | F5.1–F5.12 |
| F6 | 5 | 0 | F6.1–F6.5 |
| **Razem** | **65** | **8 lokalnie / 7 w Git** | **57 fizycznie brakujących / 58 niedostępnych z `main`** |

Brak 57 dalekosiężnych speców nie oznacza, że należy je teraz masowo wygenerować. Repozytoryjne zasady wymagają specu **przed aktywacją konkretnego feature'u**. Dla F1.2–F1.7 (P0 Fazy 1) jest to najbliższa kolejka pracy dokumentacyjnej; F1.8 ma w źródłowej roadmapie priorytet P1. F2–F6 mają szczegółowy opis epików i kryteriów w `docs/reference/full-roadmap-features.md`, ale nie mają jeszcze repozytoryjnych kontraktów implementacyjnych.

## Ustalenia według wagi

### Wysokie

**H1 — bieżąca faza i kolejka są opisane nieprawidłowo.** `README.md:9-19,139-143` kieruje do F0.1 i nazywa Fazę 0 aktywnym milestone'em. `TASKS.md:5-38` pozostawia całe F0.1–F0.7 niezaznaczone i nadal zaleca pierwsze sesje F0.1/F0.2. `AGENTS.md:25-45` ma nagłówek „Current product phase” i opisuje tylko początkową Fazę 0. Implementacja F0.7 jest już na `main`, więc nowy wykonawca może wybrać niewłaściwy scope. **Działanie:** rozdzielić „historię Fazy 0” od „aktywnej kolejki F1.1”, zaktualizować README/TASKS/AGENTS w jednym wąskim commicie dokumentacyjnym.

**H2 — F1.1 nie jest częścią śledzonego kontraktu repo.** `docs/features/F1.1-agent-adapter-contract.md` istnieje wyłącznie jako nieśledzony plik, a `docs/features/README.md:7-15` indeksuje tylko F0. Na `main` nie ma żadnego specu F1. `docs/superpowers/plans/2026-10-19-f11-agent-adapter-readiness.md` również jest nieśledzoną propozycją planu z poprzedniego przeglądu, a nie zatwierdzoną decyzją. **Działanie:** przejrzeć F1.1 względem PDF i zasad repo, następnie dodać spec i indeks do Git przed implementacją; plan zatwierdzić po rozstrzygnięciu poniższych kontraktów.

**H3 — gwarancja „retry tylko błędnej jednostki” nie ma zdefiniowanej granicy skutków ubocznych.** F1.7 w roadmapie zachowuje accepted outputs A/B/D po poprawce C; `docs/domain-model.md` deklaruje niezmienność zaakceptowanego wyniku. Jednocześnie F1.1 udostępnia jawny workspace i tylko doradczy `allowed_paths`, a izolacja Git worktree jest zaplanowana dopiero jako F3.1. Wspólny mutowalny checkout może pozwolić próbie C nadpisać pliki zaakceptowanej jednostki B mimo niezmienności wyniku w pamięci. **Działanie:** przed F1.6/F1.7 zapisać, czy gwarancja dotyczy wyłącznie wartości runtime, czy również stanu repo; dla wariantu z plikami zdefiniować minimalny mechanizm separacji lub konfliktu workspace i test skutków ubocznych. Nie przedstawiać doradczego scope jako izolacji.

### Średnie

**M1 — F1.1 wymaga świadomej interpretacji względem źródłowej roadmapy.** PDF wymienia `prepare`, `stream events`, `cancel`, `collect outputs` jako operacje interfejsu. Lokalny spec F1.1:27,179-193,298-307 odracza unified streaming do F1.3 i pozostawia `Prepare` oraz jawne `Cancel` pytaniami. To może być sensowne zawężenie, ale przy statusie „ready for implementation” musi być jawną decyzją, a nie przypadkowym pominięciem. **Działanie:** opisać minimalny kontrakt i ścieżkę rozszerzenia w ADR; skorygować status specu, jeśli decyzja nie jest jeszcze podjęta.

**M2 — granica F1.4 i F1.8 jest niejednoznaczna.** Roadmapa F1.4 (P0) wymienia `file scope check` jako typ Gate, a F1.8 (P1) osobno wymaga porównania `git diff --name-only` z `allowed_paths`, trybów warn/deny i evidence. **Działanie:** w specach określić minimalny scope check F1.4 i rozszerzenie F1.8 albo przenieść ten check w całości do F1.8; uzgodnić, co jest wymagane do exit criterion Fazy 1.

**M3 — przyszły trace i streaming wymagają nowego, ograniczonego kontraktu.** F1.2 wymaga zmienionych plików „widocznych w trace”, F1.3 wymaga postępu na żywo bez blokowania schedulera i bez niekontrolowanego RAM. Dziś `internal/events/emitter.go:48-86` zatrzymuje wszystkie eventy w pamięci oraz synchronizuje live delivery z konsumentem; `internal/shell/executor.go:108-130` trzyma pełne strumienie w `bytes.Buffer`. Eventy F0.6 nie niosą listy plików ani wyniku adaptera. **Działanie:** w specach F1.2/F1.3 określić format obserwowalnego wyniku, limit pamięci, backpressure, retencję i zgodność event schema. To jest praca F1.2/F1.3, nie powód do przebudowy F1.1 w ciemno.

**M4 — semantyka anulowania realnego agenta nie jest jeszcze określona.** `internal/shell/executor.go:102-149` kończy bezpośredni proces, lecz nie drzewo potomków; ADR-0005 to jawnie dokumentuje. F1.2 wymaga propagacji anulowania do CLI agenta, który może uruchamiać własne procesy. **Działanie:** spec F1.2 powinien zdefiniować oczekiwany zakres zakończenia procesu/sesji, limit oczekiwania i test z potomkiem.

**M5 — pokrycie testami nie działa jako dodatkowa bramka.** Przy `go test -cover` ostrzeżenie procesu pomocniczego zanieczyszcza dokładne wyniki Shell. **Działanie:** jeśli pomiar coverage stanie się wymagany, dostosować testowy helper lub jego jawne środowisko po zachowaniu testu regresji; nie zmieniać oczekiwań produkcyjnego stderr w celu ukrycia ostrzeżenia.

### Niskie / porządkowe

**L1 — statusy F0.1 i F0.2 nie odzwierciedlają Git.** `F0.1` nadal ma `ready for planning`, `F0.2` — `blocked by F0.1`, a ich kryteria są niezaznaczone, mimo że pakiety `graph` i późniejsze feature'y są na `main`. Sprawdzić kryteria w testach, potem uaktualnić statusy; nie odhaczać bez dowodów.

**L2 — nawigacja do dalszej roadmapy jest słaba.** `docs/roadmap.md:3` twierdzi, że szczegółowa roadmapa jest poza repo, chociaż `docs/reference/full-roadmap-features.md` jest w repo i pokrywa F0–F6. Compact roadmap nie pokazuje identyfikatorów F1.x–F6.x. `prompts/first-session.md` pozostaje gotowym promptem F0.1, a `prompts/implement.md:23` mówi wyłącznie o „Phase 0 runtime”. **Działanie:** dodać jawne linki i oznaczyć historyczny prompt; zrobić prompt implementacyjny neutralny wobec fazy, zachowując ochronę local-first.

**L3 — CI nie uruchamia race testów.** `.github/workflows/ci.yml:19-20` wykonuje `make check` i `make build`. F1.1 wymaga `go test -race ./...`; aktualnie to ręczna bramka. Rozważyć osobny job lub krok CI przy aktywacji F1.1, uwzględniając koszt czasu.

## Zalecana kolejność prac dokumentacyjnych

1. Zrobić jeden mały commit „Phase 0 complete / Phase 1 active”: README, TASKS, AGENTS, indeks `docs/features/`, compact roadmap i prompty. Zachować F0 jako historię oraz statusy potwierdzone testami.
2. Zweryfikować i śledzić F1.1. Zamknąć różnice względem PDF w ADR: minimalny interfejs, identyfikatory wykonania, wynik i eventy, schemat `type: agent`, cancellation, status doradczego scope. Dopiero potem zatwierdzić plan F1.1.
3. Przygotować osobne, mierzalne specy **F1.2–F1.7** w kolejności zależności z roadmapy. Dla F1.4/F1.8 oraz F1.7/F3.1 jawnie opisać granice odpowiedzialności. F1.8 tworzyć jako P1, kiedy jego zakres będzie faktycznie aktywowany.
4. Dla F2–F6 utrzymywać indeks ID, priorytet, zależności i exit criteria w roadmapie; tworzyć specy funkcjonalne just-in-time, a nie 50 szablonów bez rozstrzygnięć. Przy każdym nowym publicznym/durable kontrakcie dodać ADR.
5. Po każdym etapie wykonać `make check`, a dla zmian współbieżności `go test -race ./...`; przejrzeć diff oraz nieśledzone pliki. Naprawę trybu coverage potraktować jako osobną, małą pracę testową.

## Granice audytu

Audyt był odczytowy wobec kodu i istniejących speców; dodano tylko ten raport. Nie wykonywano implementacji F1.x ani pełnego ręcznego audytu bezpieczeństwa każdej gałęzi kodu. Zielone testy potwierdzają istniejące bramki, ale nie dowodzą jeszcze semantyki przyszłych Agent/Gate/Loop. Ścieżki i numery w ustaleniach odnoszą się do stanu `main` z 2026-10-19.
