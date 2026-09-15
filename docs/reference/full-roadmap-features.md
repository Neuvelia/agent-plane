# Agent Execution Control Plane - Roadmap i katalog feature'ów

**Cel dokumentu:** szczegółowy plan realizacji produktu od minimalnego execution core do płatnego control plane i późniejszego managed execution.

**Status:** dokument roboczy / living roadmap

> Roadmapa opisuje kolejność budowy, zakres poszczególnych feature'ów i kryteria ukończenia. Nie jest harmonogramem kalendarzowym - faza kończy się wtedy, gdy spełnione są jej exit criteria.

---

## 1. Jak czytać roadmapę

Każdy feature ma identyfikator, priorytet i cztery warstwy opisu: **cel**, **zakres funkcjonalny**, **kryteria akceptacji** oraz **granice zakresu**. Dzięki temu dokument może być używany jednocześnie jako roadmapa, materiał do rozmowy z promotorem i źródło epików do backlogu.

- **P0** - wymagane do zamknięcia fazy.
- **P1** - wysoka wartość; może wejść pod koniec fazy lub zostać przesunięte do następnej bez łamania podstawowego milestone'u.
- **P2** - świadomie odłożone na później.

## 2. Zasady, które roadmapa ma chronić

- **Local-first.** Podstawowa wartość - graph authoring, runner, loops, gates i debugging - działa bez konta cloud.
- **Graph jako kontrakt.** Graph file jest wersjonowalny i uruchamialny poza UI; canvas nie jest jedynym źródłem prawdy.
- **Node != agent.** Shell, Gate, Human i później Code są równoprawnymi elementami; LLM nie jest używany do deterministycznych transformacji.
- **Retry najmniejszej jednostki.** Czerwony unit nie niszczy poprawnych wyników innych gałęzi.
- **Evidence przed confidence.** Gate opiera się najpierw na testach, exit code, diff scope i innych programowalnych sygnałach.
- **Human gate wg blast radius.** Człowiek pojawia się przy działaniach o dużym koszcie błędu lub niskiej odwracalności.
- **Trace jest produktem.** Run history, attempty, reason/evidence i decyzje człowieka nie są dodatkiem diagnostycznym.
- **Cloud jako control plane.** Chmura ma dawać zdalność, ciągłość, historię i collaboration; kod może pozostać na runnerze lokalnym.

## 3. Mapa wydań

| Faza | Wydanie | Główny rezultat |
|---|---|---|
| 0 | v0.1 - Execution Core | Udowodnić semantykę graph/node/edge/run bez inwestowania w UI. |
| 1 | v0.2 - Agent Loop | Uruchomić prawdziwego coding agenta jako node, wprowadzić deterministyczny gate i umożliwić korektę wyłącznie niepoprawnej jednostki pracy. |
| 2 | v0.3 - Local IDE | Przenieść authoring, obserwację i debugowanie z terminala do lokalnego IDE, zachowując plikowy/wersjonowalny model graphu jako źródło prawdy. |
| 3 | v0.4 - Local Production Ready | Przekształcić demo IDE w runtime, któremu można powierzyć dłuższe i bardziej ryzykowne zadania bez utraty kontroli po awarii, restarcie lub błędzie agenta. |
| 4 | v0.5 - Connected Control Plane | Dodać zdalny nadzór nad lokalnymi runnerami bez obowiązkowego wysyłania kodu repo do chmury. |
| 5 | v1.0 - Pro / Team | Monetyzować ciągłość, zdalność, historię i współpracę - nie sztuczne limity lokalnego authoringu. |
| 6 | Post-v1 | Dopiero po potwierdzeniu popytu na control plane dołożyć kosztowną warstwę wykonywania nieufnego kodu klientów i funkcje enterprise. |

### Kolejność zależności

```text
Graph schema + validator
        ↓
Scheduler + Shell + events
        ↓
Agent adapter + Gate + Loop
        ↓
Local IDE + Run Inspector
        ↓
Durability + isolation + security
        ↓
Cloud connection + remote control
        ↓
Team / billing / policy / analytics
        ↓
Managed execution + enterprise
```

## 4.0. Faza 0 - Fundament domeny i CLI

**Target release:** v0.1 - Execution Core

**Cel fazy:** Udowodnić semantykę graph/node/edge/run bez inwestowania w UI. Po tej fazie system ma umieć deterministycznie zaplanować DAG, wykonać shellowe jednostki pracy i opisać przebieg za pomocą zdarzeń.

### Zakres feature'ów

| ID | Feature | Priorytet |
|---|---|:---:|
| F0.1 | Graph Definition Schema v0 | P0 |
| F0.2 | Graph Validator i lint | P0 |
| F0.3 | Data bindings na edge'ach | P0 |
| F0.4 | DAG Scheduler in-memory | P0 |
| F0.5 | Shell Node | P0 |
| F0.6 | Event Model i Run/Node IDs | P0 |
| F0.7 | CLI Runner | P0 |

### F0.1. Graph Definition Schema v0

**Priorytet:** P0

**Po co:** Stabilny, wersjonowany kontrakt opisujący graph tak, aby ten sam plik można było walidować, wersjonować w Git i uruchamiać z CLI lub później z UI.

**Zakres funkcjonalny**

- Format YAML jako podstawowy format authoringu; JSON jako reprezentacja równoważna.
- Pola: graph id, version, inputs, nodes, edges, outputs i metadata.
- Node ma jawne id, type, input mapping, config, timeout policy i output contract.
- Edge odwołuje się do konkretnego outputu źródła i inputu celu; brak „gołych” strzałek bez przepływu danych.
- Pole schema_version umożliwia późniejsze migracje.

**Kryteria akceptacji**

- Niepoprawny typ node'a, brakujący input i cykl dają czytelny błąd z lokalizacją.
- Ten sam plik po serializacji i ponownym odczycie zachowuje znaczenie.
- Przykładowy graph trzech node'ów przechodzi walidację bez ostrzeżeń.

**Poza zakresem tej iteracji:** Brak dynamicznych warunków i zaawansowanego DSL. Najpierw minimalny DAG.

### F0.2. Graph Validator i lint

**Priorytet:** P0

**Po co:** Wykrywać błędy przed uruchomieniem i stopniowo uczyć użytkownika poprawnego modelowania zależności.

**Zakres funkcjonalny**

- Walidacja strukturalna schematu.
- Walidacja referencji do node'ów, inputów i outputów.
- Detekcja cykli na poziomie głównego graphu.
- Ostrzeżenie dla edge'a, którego output nie jest używany przez kolejny node.
- Ostrzeżenia nie blokują runa; błędy P0 blokują.

**Kryteria akceptacji**

- CLI `graph validate` zwraca exit code 0 dla poprawnego graphu i !=0 dla błędnego.
- Komunikat zawiera feature/node id i sugestię naprawy.
- Validator ma testy dla cyklu, brakującego node'a i błędnego mappingu.

**Poza zakresem tej iteracji:** Autofix i inteligentne sugestie z LLM pozostają poza fazą.

### F0.3. Data bindings na edge'ach

**Priorytet:** P0

**Po co:** Rozdzielić kolejność od rzeczywistej zależności danych, aby scheduler mógł bezpiecznie uruchamiać niezależne node'y równolegle.

**Zakres funkcjonalny**

- Binding w stylu `from: planner.outputs.backend_units` -> `to: backend.inputs.units`.
- Wsparcie prostych wartości skalarnych, JSON i referencji do artifactów.
- Jawny błąd przy niezgodnym typie lub braku wymaganej wartości.
- Brak ukrytego współdzielonego kontekstu między node'ami.

**Kryteria akceptacji**

- Scheduler może wywnioskować gotowość node'a wyłącznie z jego zależności i inputów.
- Dwa node'y bez zależności uruchamiają się równolegle.

**Poza zakresem tej iteracji:** Transformacje map/filter/join jako osobne Code node'y, nie jako rozbudowany język bindingów.

### F0.4. DAG Scheduler in-memory

**Priorytet:** P0

**Po co:** Zapewnić poprawne wykonanie grafu z równoległością i propagacją stanów.

**Zakres funkcjonalny**

- Stany: pending, ready, running, succeeded, failed, skipped, cancelled.
- Topologiczne uruchamianie gotowych node'ów.
- Konfigurowalny limit globalnej współbieżności.
- Fail-fast na błędzie nieobsługiwanym przez graph.
- Prosty merge wyników do graph outputs.

**Kryteria akceptacji**

- Dwie niezależne gałęzie faktycznie nakładają się czasowo.
- Node zależny nie startuje przed zakończeniem wymaganych poprzedników.
- Run kończy się jednoznacznym terminalnym stanem.

**Poza zakresem tej iteracji:** Brak trwałego recovery po restarcie; to F3.2/F3.3.

### F0.5. Shell Node

**Priorytet:** P0

**Po co:** Mieć pierwszy deterministyczny typ node'a, który pozwala testować cały runtime bez zależności od LLM.

**Zakres funkcjonalny**

- Command, args, cwd i env allowlist.
- Capture stdout, stderr i exit code.
- Limit rozmiaru logu i zachowanie pełnego logu jako artifact opcjonalny.
- Możliwość zwrócenia prostego JSON outputu.

**Kryteria akceptacji**

- `exit 0` daje succeeded, niezerowy exit daje failed.
- stdout/stderr są widoczne w event streamie.
- Proces potomny jest sprzątany po przerwaniu runa.

**Poza zakresem tej iteracji:** Zaawansowany sandbox i Docker dopiero po stabilizacji podstawowego runtime.

### F0.6. Event Model i Run/Node IDs

**Priorytet:** P0

**Po co:** Każde wykonanie ma być od początku obserwowalne i możliwe do odtworzenia z eventów.

**Zakres funkcjonalny**

- UUID/ULID dla graph_run, node_run i attempt.
- Minimalne eventy: run_started, node_ready, node_started, log_chunk, node_finished, run_finished.
- Każdy event ma timestamp, sequence, graph_version i correlation ids.
- Eventy nie zawierają sekretów; runtime stosuje redaction hook.

**Kryteria akceptacji**

- Dla każdego runa da się odtworzyć kolejność wykonania.
- Event sequence jest monotoniczny w obrębie runa.
- Brak eventu terminalnego jest rozpoznawany jako run przerwany.

**Poza zakresem tej iteracji:** Pełny append-only storage pojawi się w fazie 3.

### F0.7. CLI Runner

**Priorytet:** P0

**Po co:** Dać developerowi najszybszy interfejs do uruchamiania i debugowania graphów zanim powstanie IDE.

**Zakres funkcjonalny**

- `agentplane run graph.yaml --input key=value`.
- `agentplane graph validate`.
- Tryb `--watch` z czytelnymi stanami node'ów i czasami.
- Exit code procesu odpowiada rezultatowi graph runa.
- Flaga `--json` do outputu maszynowego.

**Kryteria akceptacji**

- Typowy graph można zwalidować i uruchomić bez UI.
- CI może użyć exit code i JSON outputu.

**Poza zakresem tej iteracji:** Interactive TUI pozostaje poza MVP.

### Exit criteria fazy

> **Faza 0 jest zakończona, gdy:** Graph z trzema Shell node'ami wykonuje dwie niezależne gałęzie równolegle, poprawnie przekazuje dane, emituje eventy i kończy się deterministycznym wynikiem.

## 4.1. Faza 1 - Agent execution, gate i lokalna pętla korekcyjna

**Target release:** v0.2 - Agent Loop

**Cel fazy:** Uruchomić prawdziwego coding agenta jako node, wprowadzić deterministyczny gate i umożliwić korektę wyłącznie niepoprawnej jednostki pracy.

### Zakres feature'ów

| ID | Feature | Priorytet |
|---|---|:---:|
| F1.1 | Agent Adapter Contract | P0 |
| F1.2 | Pierwszy coding-agent adapter | P0 |
| F1.3 | Unified Streaming | P0 |
| F1.4 | Gate Node | P0 |
| F1.5 | Check Result Contract | P0 |
| F1.6 | Correction Loop + maxAttempts | P0 |
| F1.7 | Return Envelope / scoped retry | P0 |
| F1.8 | Changed-files Scope Check | P1 |

### F1.1. Agent Adapter Contract

**Priorytet:** P0

**Po co:** Oddzielić runtime od konkretnego providera/CLI, aby produkt nie był wrapperem jednego coding agenta.

**Zakres funkcjonalny**

- Interfejs: detect capabilities, prepare, execute, stream events, cancel, collect outputs.
- Ujednolicony input: task, workspace, allowed paths, context refs, environment refs.
- Ujednolicony output: summary, changed files, artifacts, raw metadata, usage metadata.
- Adapter deklaruje capabilities zamiast runtime zakładającego ich obecność.

**Kryteria akceptacji**

- Mock adapter przechodzi kontraktowe testy.
- Runtime nie ma kodu specyficznego dla konkretnego providera poza package adaptera.

**Poza zakresem tej iteracji:** Nie normalizować wszystkich różnic providerów na siłę; raw metadata pozostaje dostępne.

### F1.2. Pierwszy coding-agent adapter

**Priorytet:** P0

**Po co:** Przeprowadzić end-to-end pracę na realnym repo i udowodnić, że abstrakcja adaptera jest wystarczająca.

**Zakres funkcjonalny**

- Integracja z jednym realnie używanym coding agentem CLI.
- Detekcja instalacji i wersji.
- Przekazanie workspace, tasku i scope.
- Streaming tekstu/statusów.
- Odczyt listy zmienionych plików z Git, niezależnie od deklaracji modelu.

**Kryteria akceptacji**

- Agent zmienia plik w repo testowym i wynik jest widoczny w trace.
- Brak zainstalowanego CLI daje actionable error, nie panic.
- Anulowanie propaguje się do procesu agenta.

**Poza zakresem tej iteracji:** Drugi adapter jest P1 dopiero po stabilizacji kontraktu.

### F1.3. Unified Streaming

**Priorytet:** P0

**Po co:** Pokazywać przebieg pracy agenta w czasie rzeczywistym bez wiązania UI z formatem konkretnego CLI.

**Zakres funkcjonalny**

- Eventy: text_delta, tool_call_started/finished, command_started/finished, file_changed, status.
- Normalizacja do wspólnego event envelope.
- Raw provider event może być dołączony jako debug payload.
- Backpressure i limit pamięci dla długich streamów.

**Kryteria akceptacji**

- CLI pokazuje postęp zanim agent skończy.
- Stream nie blokuje schedulera.
- Duży output nie powoduje niekontrolowanego wzrostu RAM.

**Poza zakresem tej iteracji:** Pełna semantyczna rekonstrukcja „myśli” modelu nie jest celem produktu.

### F1.4. Gate Node

**Priorytet:** P0

**Po co:** Wprowadzić jawny punkt kontrolny, który może zatrzymać błędny rezultat zanim pójdzie dalej.

**Zakres funkcjonalny**

- Gate konsumuje output poprzedniego node'a i zwraca verdict green/red.
- Typy gate v1: command exit code, file scope check, JSON predicate.
- Gate zwraca reason i evidence w strukturze.
- Gate nie modyfikuje workspace.

**Kryteria akceptacji**

- Failing test daje verdict red i blokuje downstream.
- Evidence zawiera co najmniej check id, exit code oraz skrócony log.
- Green gate przepuszcza dokładnie raz.

**Poza zakresem tej iteracji:** LLM-as-judge może być dodatkiem, ale nie domyślnym kryterium bezpieczeństwa.

### F1.5. Check Result Contract

**Priorytet:** P0

**Po co:** Ustandaryzować komunikat zwrotny tak, aby agent dostawał konkretny błąd, a UI mogło go pokazać bez parsowania tekstu.

**Zakres funkcjonalny**

- Pola: verdict, reason, evidence[], scope, check_id, duration.
- Evidence może wskazywać plik/linijkę, test, diff lub artifact.
- Reason krótki i stabilny; pełny output jako artifact.

**Kryteria akceptacji**

- Każdy red gate zwraca niepusty reason.
- Check result jest serializowalny i zapisuje się w run trace.

**Poza zakresem tej iteracji:** Brak uniwersalnego confidence score w kryterium przejścia.

### F1.6. Correction Loop + maxAttempts

**Priorytet:** P0

**Po co:** Pozwolić node'owi samodzielnie poprawić wynik po nieudanym gate, ale ograniczyć koszty i ryzyko nieskończonej pętli.

**Zakres funkcjonalny**

- Konfiguracja max_attempts, default 3.
- Każda próba ma osobny attempt id i trace.
- Agent dostaje poprzedni check result i jasno ograniczony scope.
- Po wyczerpaniu prób node przechodzi w failed_needs_intervention.

**Kryteria akceptacji**

- Nie ma więcej prób niż limit.
- Każda próba jest widoczna osobno.
- Downstream widzi tylko zaakceptowany output, nigdy czerwony attempt.

**Poza zakresem tej iteracji:** Adaptacyjne limity i automatyczny replanning później.

### F1.7. Return Envelope / scoped retry

**Priorytet:** P0

**Po co:** Zapobiec przepisywaniu poprawnych części pracy, kiedy odrzucana jest tylko jedna jednostka.

**Zakres funkcjonalny**

- Envelope: unit, verdict, reason, evidence, scope, attempt.
- Scope może być listą ścieżek lub logicznym unit id.
- Retry tworzy nowy attempt tylko dla odrzuconego unit/node.
- Runtime blokuje retry całego batcha, jeśli graph deklaruje retry per-unit.

**Kryteria akceptacji**

- W teście A/B/C/D po błędzie C ponownie wykonuje się wyłącznie C.
- A, B i D zachowują wcześniejsze accepted outputs.

**Poza zakresem tej iteracji:** Cross-unit replanning to osobny mechanizm, nie ukryte zachowanie retry.

### F1.8. Changed-files Scope Check

**Priorytet:** P1

**Po co:** Mechanicznie egzekwować, że korekta nie wychodzi poza przydzielony zakres.

**Zakres funkcjonalny**

- Porównanie `git diff --name-only` z allowed paths.
- Tryb warn i deny.
- Evidence wskazuje pliki spoza scope.

**Kryteria akceptacji**

- Agent zmieniający niedozwolony plik dostaje red verdict.
- Dozwolone pliki nie są fałszywie odrzucane.

**Poza zakresem tej iteracji:** Semantyczny blast radius z analizą AST później.

### Exit criteria fazy

> **Faza 1 jest zakończona, gdy:** Agent modyfikuje repo, deterministyczny gate wykrywa błąd, runtime zwraca reason/evidence/scope i agent poprawia tylko swoją jednostkę w maksymalnie trzech próbach.

## 4.2. Faza 2 - Local IDE i debugger wykonania

**Target release:** v0.3 - Local IDE

**Cel fazy:** Przenieść authoring, obserwację i debugowanie z terminala do lokalnego IDE, zachowując plikowy/wersjonowalny model graphu jako źródło prawdy.

### Zakres feature'ów

| ID | Feature | Priorytet |
|---|---|:---:|
| F2.1 | Graph Canvas | P0 |
| F2.2 | Node Inspector | P0 |
| F2.3 | Edge/Data Mapping Editor | P0 |
| F2.4 | Graph Validation UI | P0 |
| F2.5 | Live Execution Map | P0 |
| F2.6 | Run Inspector - Overview | P0 |
| F2.7 | Run Inspector - Node Detail | P0 |
| F2.8 | Attempt Compare | P1 |
| F2.9 | Local Run History + SQLite | P0 |
| F2.10 | Human Gate UI | P0 |
| F2.11 | Onboarding + Capability Detection | P1 |
| F2.12 | Graph Templates | P1 |

### F2.1. Graph Canvas

**Priorytet:** P0

**Po co:** Pozwalać tworzyć i czytać workflow jako graf, bez ukrywania jego reprezentacji tekstowej.

**Zakres funkcjonalny**

- Canvas z zoom/pan, drag-drop node'ów i portami input/output.
- Node'y pokazują type, label, status i najważniejszą konfigurację.
- Połączenie jest możliwe tylko między kompatybilnymi portami.
- Zmiana canvasu aktualizuje graph definition.
- Layout zapisuje się jako metadata i nie wpływa na semantykę.

**Kryteria akceptacji**

- Graph stworzony w UI można uruchomić z CLI bez modyfikacji.
- Przesunięcie node'a nie zmienia hash semantycznej definicji.

**Poza zakresem tej iteracji:** Zaawansowany auto-layout i grupy wizualne mogą być P1.

### F2.2. Node Inspector

**Priorytet:** P0

**Po co:** Dać jedno miejsce do konfiguracji node'a i wyjaśnić użytkownikowi, co dokładnie wejdzie i wyjdzie z jednostki pracy.

**Zakres funkcjonalny**

- Pola zależne od typu node'a.
- Sekcje: inputs, execution, checks/loop, outputs, permissions.
- Walidacja inline.
- Podgląd resolved input przed runem tam, gdzie to możliwe.

**Kryteria akceptacji**

- Nie da się zapisać konfiguracji niespełniającej schematu bez widocznego błędu.
- Zmiana konfiguracji jest natychmiast odzwierciedlona w pliku graphu.

**Poza zakresem tej iteracji:** Form builder dla dowolnych pluginów później.

### F2.3. Edge/Data Mapping Editor

**Priorytet:** P0

**Po co:** Uczynić dependency jawnie zależną od danych i redukować przypadkowe sekwencyjne chainy.

**Zakres funkcjonalny**

- UI pokazuje output źródłowy i input docelowy.
- Proste mapowanie 1:1 oraz select z JSON object.
- Ostrzeżenie „target does not consume source output”.
- Sugestia możliwej równoległości przy pustym bindingu.

**Kryteria akceptacji**

- Użytkownik potrafi wskazać konkretną wartość przechodzącą przez edge.
- Nieprawidłowy binding jest zaznaczony przed runem.

**Poza zakresem tej iteracji:** Graficzne ETL nie jest celem; złożone transformacje trafiają do Code node.

### F2.4. Graph Validation UI

**Priorytet:** P0

**Po co:** Przenieść wyniki validatora do kontekstu canvasu, aby błędy były naprawialne bez czytania logów.

**Zakres funkcjonalny**

- Panel issues z severity i filtrowaniem.
- Kliknięcie issue fokusuje node/edge.
- Inline badge na problematycznym elemencie.
- Run button disabled tylko dla błędów blokujących.

**Kryteria akceptacji**

- Każdy błąd CLI ma odpowiednik w UI.
- Użytkownik może przejść z issue do źródła problemu jednym kliknięciem.

**Poza zakresem tej iteracji:** Automatyczne poprawki P2.

### F2.5. Live Execution Map

**Priorytet:** P0

**Po co:** Pokazać stan systemu jako żywy graf, nie jako ścianę logów.

**Zakres funkcjonalny**

- Status node'a: waiting, ready, running, retrying, green, red, human_waiting.
- Edge pokazuje aktywny przepływ danych.
- Badge attempt count i duration.
- Równoległe node'y są widoczne jako jednocześnie running.

**Kryteria akceptacji**

- Stan UI jest rekonstruowany z event streamu, a nie z lokalnych timeoutów.
- Po odświeżeniu można odtworzyć stan z zapisanych eventów.

**Poza zakresem tej iteracji:** Animacje są drugorzędne wobec czytelności.

### F2.6. Run Inspector - Overview

**Priorytet:** P0

**Po co:** Odpowiadać na pytania: co się wydarzyło, gdzie system czekał, co zawiodło i ile to kosztowało czasu.

**Zakres funkcjonalny**

- Timeline node'ów i attemptów.
- Wall-clock time, sumaryczny agent time, estimated parallelism saved.
- Status całego runa i critical path w wersji podstawowej.
- Filtry: failed, retried, human, slow.

**Kryteria akceptacji**

- Kliknięcie node'a otwiera jego detail bez utraty kontekstu runa.
- Retry jest pokazany jako kolejne attempt, nie jako nadpisany log.

**Poza zakresem tej iteracji:** Zaawansowana analiza critical path P1.

### F2.7. Run Inspector - Node Detail

**Priorytet:** P0

**Po co:** Dać pełne evidence dla pojedynczej decyzji wykonawczej.

**Zakres funkcjonalny**

- Resolved input.
- Normalized agent/tool events.
- Output i artifacts.
- Check results z reason/evidence.
- Lista changed files i diff summary.
- Attempt selector.

**Kryteria akceptacji**

- Dla czerwonego gate można bez szukania w logu wskazać reason i evidence.
- Dla retry można porównać wejście i rezultat kolejnych prób.

**Poza zakresem tej iteracji:** Pełny diff viewer może bazować na istniejącym komponencie, bez budowy edytora Git od zera.

### F2.8. Attempt Compare

**Priorytet:** P1

**Po co:** Ułatwić zrozumienie, czy retry naprawił wskazany problem czy tylko zmienił wynik w przypadkowy sposób.

**Zakres funkcjonalny**

- Side-by-side: reason/evidence, changed files, duration.
- Diff między attemptami.
- Badge „scope expanded” jeśli druga próba dotknęła nowych plików.

**Kryteria akceptacji**

- Porównanie dwóch prób jest dostępne z node detail.

**Poza zakresem tej iteracji:** Semantyczne porównanie LLM-em P2.

### F2.9. Local Run History + SQLite

**Priorytet:** P0

**Po co:** Zachować historię wykonania bez wymagania konta cloud.

**Zakres funkcjonalny**

- Lista runów per repo/graph.
- Status, start, duration, graph version, branch/commit.
- SQLite przechowuje metadata/event index; duże artifacts na filesystemie.
- Retention konfigurowalny lokalnie.

**Kryteria akceptacji**

- Restart aplikacji nie usuwa historii.
- Kliknięcie starego runa odtwarza inspector.

**Poza zakresem tej iteracji:** Cloud sync dopiero F4.6.

### F2.10. Human Gate UI

**Priorytet:** P0

**Po co:** Zatrzymać graph w miejscu wymagającym decyzji człowieka i pokazać mu tylko potrzebny kontekst.

**Zakres funkcjonalny**

- Stan human_waiting.
- Panel: blast radius, checks, changed files, diff link, reason for approval.
- Akcje approve/reject z opcjonalnym komentarzem.
- Decyzja emituje event i jest audytowalna lokalnie.

**Kryteria akceptacji**

- Graph nie kontynuuje bez decyzji.
- Reject może zakończyć run lub skierować do jawnie skonfigurowanej ścieżki.

**Poza zakresem tej iteracji:** Granularne role użytkowników dopiero Team.

### F2.11. Onboarding + Capability Detection

**Priorytet:** P1

**Po co:** Skrócić drogę od instalacji do pierwszego działającego graphu.

**Zakres funkcjonalny**

- Wykrycie Git, shell, Dockera opcjonalnie i wspieranych coding-agent CLI.
- Lista repo z ręcznym dodaniem folderu.
- Self-check runnera.
- Starter graph „modify -> test -> approve”.

**Kryteria akceptacji**

- Nowy użytkownik może uruchomić starter graph bez ręcznej edycji configu.

**Poza zakresem tej iteracji:** Automatyczne skanowanie całego dysku nie jest wymagane.

### F2.12. Graph Templates

**Priorytet:** P1

**Po co:** Przyspieszyć pierwsze użycie i pokazać poprawne wzorce graph/loop/gate.

**Zakres funkcjonalny**

- Templates: fix issue, code review, dependency update, repo audit.
- Template kopiuje się do repo jako zwykły graph file.
- Każdy template ma wymagane capabilities.

**Kryteria akceptacji**

- Template można zmodyfikować bez lock-in do generatora.

**Poza zakresem tej iteracji:** Publiczny marketplace poza MVP.

### Exit criteria fazy

> **Faza 2 jest zakończona, gdy:** Developer projektuje graph, uruchamia go, obserwuje live execution, analizuje reason/evidence, obsługuje human gate i wraca do starszego runa bez używania terminala poza uruchomieniem aplikacji.

## 4.3. Faza 3 - Lokalna produkcyjność i bezpieczeństwo wykonania

**Target release:** v0.4 - Local Production Ready

**Cel fazy:** Przekształcić demo IDE w runtime, któremu można powierzyć dłuższe i bardziej ryzykowne zadania bez utraty kontroli po awarii, restarcie lub błędzie agenta.

### Zakres feature'ów

| ID | Feature | Priorytet |
|---|---|:---:|
| F3.1 | Workspace Isolation / Git Worktrees | P0 |
| F3.2 | Durable Run State / Append-only Event Store | P0 |
| F3.3 | Resume / Recovery | P0 |
| F3.4 | Cancellation i Timeouts | P0 |
| F3.5 | Artifact Store Local | P0 |
| F3.6 | Graph Versioning i Run Reproducibility | P0 |
| F3.7 | Export JSON/CSV | P0 |
| F3.8 | Secrets References | P0 |
| F3.9 | Permissions / Execution Policy v0 | P0 |
| F3.10 | Cost/Usage Metadata | P1 |
| F3.11 | Local Diagnostics | P1 |

### F3.1. Workspace Isolation / Git Worktrees

**Priorytet:** P0

**Po co:** Odseparować równoległe jednostki pracy i retry tak, aby nie nadpisywały sobie zmian.

**Zakres funkcjonalny**

- Per-node/per-unit worktree lub izolowany checkout.
- Jawny base commit.
- Merge strategy po zaakceptowaniu outputu.
- Automatyczne sprzątanie z możliwością retain-on-failure.

**Kryteria akceptacji**

- Dwa równoległe node'y mogą zmienić ten sam plik bez wyścigu na filesystemie.
- Niezaakceptowany attempt nie trafia do głównego workspace.

**Poza zakresem tej iteracji:** Zaawansowane semantic merge/rebase może pozostać manualne.

### F3.2. Durable Run State / Append-only Event Store

**Priorytet:** P0

**Po co:** Nie tracić informacji o runie w przypadku crasha aplikacji lub runnera.

**Zakres funkcjonalny**

- Eventy zapisywane trwale przed uznaniem transition za zakończony.
- Snapshot stanu jako optymalizacja, event log jako źródło audytu.
- Wersjonowanie event schema.
- Detekcja runów przerwanych.

**Kryteria akceptacji**

- Kill procesu w środku runa nie usuwa historii dotychczasowych kroków.
- Stan można zrekonstruować z eventów.

**Poza zakresem tej iteracji:** Rozproszony event bus nie jest potrzebny lokalnie.

### F3.3. Resume / Recovery

**Priorytet:** P0

**Po co:** Kontynuować run po restarcie bez ponownego wykonywania zaakceptowanych node'ów.

**Zakres funkcjonalny**

- Recovery plan identyfikuje terminalne, bezpiecznie wznawialne i niepewne node'y.
- Użytkownik zatwierdza wznowienie node'ów z side effects.
- Accepted outputs są ponownie używane.

**Kryteria akceptacji**

- Po restarcie run z trzema zakończonymi node'ami nie wykonuje ich drugi raz.
- Niepewny command z side effect nie jest automatycznie replayowany.

**Poza zakresem tej iteracji:** Exactly-once side effects nie są obiecywane; celem jest jawna semantyka recovery.

### F3.4. Cancellation i Timeouts

**Priorytet:** P0

**Po co:** Dać użytkownikowi i politykom twardą możliwość zatrzymania pracy.

**Zakres funkcjonalny**

- Timeout per node i per graph.
- Graceful cancel -> hard kill po grace period.
- Propagacja cancel do procesów potomnych.
- Cancelled ma osobny terminalny status.

**Kryteria akceptacji**

- Po cancel nie pozostają procesy potomne.
- Timeout emituje reason i jest odróżnialny od zwykłego failure.

**Poza zakresem tej iteracji:** Zaawansowane quota scheduling później.

### F3.5. Artifact Store Local

**Priorytet:** P0

**Po co:** Przechowywać duże i strukturalne wyniki poza samym event logiem.

**Zakres funkcjonalny**

- Artifact metadata: id, type, size, hash, producer, retention.
- Typy: log, diff, JSON, report, patch, test output.
- Content-addressed lub hash-based dedup opcjonalny.
- Event zawiera reference, nie cały blob.

**Kryteria akceptacji**

- Run inspector otwiera artifact po restarcie.
- Usunięty artifact jest pokazany jako expired/missing, nie powoduje crasha.

**Poza zakresem tej iteracji:** Cloud object storage dopiero F4.10.

### F3.6. Graph Versioning i Run Reproducibility

**Priorytet:** P0

**Po co:** Wiedzieć dokładnie, jaka definicja graphu uruchomiła dany run.

**Zakres funkcjonalny**

- Hash semantycznej definicji.
- Snapshot graph definition przy runie.
- Repo commit/branch i runtime version w metadata.
- Prompt/config versions w node snapshot.

**Kryteria akceptacji**

- Stary run wskazuje dokładny graph snapshot nawet po zmianie pliku.
- Dwa runy z różnymi graphami mają różne wersje.

**Poza zakresem tej iteracji:** Pełna deterministyczna reprodukcja LLM outputu nie jest możliwa i nie jest obiecywana.

### F3.7. Export JSON/CSV

**Priorytet:** P0

**Po co:** Umożliwić analizę eksperymentów, backup i integrację z innymi narzędziami.

**Zakres funkcjonalny**

- JSON: pełny run summary + node/attempt/check metadata.
- CSV: płaska tabela attemptów i metryk.
- Opcjonalny bundle manifest artifactów.
- Stabilna wersja export schema.

**Kryteria akceptacji**

- Eksport da się wczytać niezależnym skryptem bez dostępu do SQLite.
- CSV zawiera duration, attempts, verdict, human intervention i usage jeśli dostępne.

**Poza zakresem tej iteracji:** Eksport surowych sekretów jest zabroniony.

### F3.8. Secrets References

**Priorytet:** P0

**Po co:** Nie zapisywać tokenów/API keys w graphach i run history.

**Zakres funkcjonalny**

- Graph przechowuje tylko `secret_ref`.
- Pierwszy provider: OS keychain lub env injected at runtime.
- Redaction w logach i eventach.
- Node dostaje tylko jawnie przypisane sekrety.

**Kryteria akceptacji**

- Sekret nie pojawia się w YAML, SQLite ani standardowym eksporcie.
- Brak secret_ref daje czytelny preflight error.

**Poza zakresem tej iteracji:** Centralny cloud secrets vault dopiero Team/Business.

### F3.9. Permissions / Execution Policy v0

**Priorytet:** P0

**Po co:** Ograniczyć możliwości node'a zgodnie z zasadą least privilege.

**Zakres funkcjonalny**

- Allowed paths, network on/off, shell on/off, allowed commands w wersji podstawowej.
- Policy jest częścią node config lub graph defaults.
- Preflight pokazuje wymagane permissions.
- Naruszenie policy daje blocked/red z evidence.

**Kryteria akceptacji**

- Node nie może pisać poza allowed paths w trybie enforce.
- UI pokazuje requested permissions przed runem.

**Poza zakresem tej iteracji:** Pełny policy language i organizacyjne policies dopiero F5.9.

### F3.10. Cost/Usage Metadata

**Priorytet:** P1

**Po co:** Mierzyć koszt systemu agentowego tam, gdzie adapter udostępnia dane.

**Zakres funkcjonalny**

- Tokens in/out, provider cost jeśli dostępny, tool duration, agent duration.
- Brak danych oznaczony jako unknown, nie jako 0.
- Agregacja per node/run/graph.

**Kryteria akceptacji**

- UI nie myli unknown z zero.
- Eksport zawiera usage metadata.

**Poza zakresem tej iteracji:** Nie obiecywać identycznego modelu kosztu między providerami.

### F3.11. Local Diagnostics

**Priorytet:** P1

**Po co:** Ułatwić zgłaszanie problemów z runnerem bez wysyłania kodu repo.

**Zakres funkcjonalny**

- Health report: runtime version, OS, capabilities, adapter versions, DB health.
- Redacted diagnostic bundle.
- Self-test command.

**Kryteria akceptacji**

- Bundle nie zawiera repo contents ani sekretów domyślnie.

**Poza zakresem tej iteracji:** Remote support tooling później.

### Exit criteria fazy

> **Faza 3 jest zakończona, gdy:** Run może być zatrzymany i wznowiony po restarcie, równoległe node'y działają w izolowanych workspace'ach, trace/artifacts są trwałe, a sekrety i permissions mają jawny model.

## 4.4. Faza 4 - Cloud Control Plane Alpha

**Target release:** v0.5 - Connected Control Plane

**Cel fazy:** Dodać zdalny nadzór nad lokalnymi runnerami bez obowiązkowego wysyłania kodu repo do chmury. Cloud przechowuje przede wszystkim metadata, presence i decyzje sterujące.

### Zakres feature'ów

| ID | Feature | Priorytet |
|---|---|:---:|
| F4.1 | Account + Authentication | P0 |
| F4.2 | Projects | P0 |
| F4.3 | Runner Enrollment | P0 |
| F4.4 | Outbound Secure Connection | P0 |
| F4.5 | Runner Presence & Health | P0 |
| F4.6 | Remote Run Start | P0 |
| F4.7 | Cloud Run Metadata Sync | P0 |
| F4.8 | Web Human Approval | P0 |
| F4.9 | Cloud Security Baseline | P0 |
| F4.10 | Optional Artifact Sync | P1 |

### F4.1. Account + Authentication

**Priorytet:** P0

**Po co:** Umożliwić bezpieczne powiązanie webowego control plane z użytkownikiem i jego runnerami.

**Zakres funkcjonalny**

- Email/OAuth jako minimalny login.
- Session management i revoke.
- Account deletion flow.
- Brak wymogu konta dla Local Free.

**Kryteria akceptacji**

- Lokalna aplikacja działa bez logowania.
- Po wylogowaniu token cloud jest usuwany z klienta.

**Poza zakresem tej iteracji:** SSO/SAML dopiero enterprise.

### F4.2. Projects

**Priorytet:** P0

**Po co:** Grupować runner metadata, graphy i runy według projektu/repo bez wprowadzania jeszcze pełnych organizacji.

**Zakres funkcjonalny**

- Project id/name, optional repo identity, retention settings.
- Graph metadata przypisane do projektu.
- Runner może być dostępny dla wielu projektów użytkownika.

**Kryteria akceptacji**

- Remote run musi jawnie wskazywać project i runner.

**Poza zakresem tej iteracji:** Organizations/team membership dopiero faza 5.

### F4.3. Runner Enrollment

**Priorytet:** P0

**Po co:** Bezpiecznie sparować lokalny daemon z kontem cloud.

**Zakres funkcjonalny**

- Jednorazowy enrollment token/device code.
- Runner generuje własny identity key.
- Nazwa, platforma, capabilities, last_seen.
- Możliwość revoke z web i lokalnie.

**Kryteria akceptacji**

- Skradziony jednorazowy kod wygasa.
- Revoked runner nie może pobierać nowych poleceń.

**Poza zakresem tej iteracji:** Automatyczne fleet provisioning później.

### F4.4. Outbound Secure Connection

**Priorytet:** P0

**Po co:** Sterować runnerem bez otwierania portów na laptopie użytkownika.

**Zakres funkcjonalny**

- Runner inicjuje TLS WebSocket/stream do backendu.
- Heartbeat/presence.
- Reconnection z exponential backoff.
- Command envelope podpisany/autoryzowany i idempotency key.

**Kryteria akceptacji**

- Router/NAT użytkownika nie wymaga zmian.
- Po utracie internetu lokalny run może kontynuować zgodnie z policy.

**Poza zakresem tej iteracji:** Peer-to-peer transport nie jest potrzebny w alpha.

### F4.5. Runner Presence & Health

**Priorytet:** P0

**Po co:** Pokazać, czy dany target jest gotowy do przyjęcia runa.

**Zakres funkcjonalny**

- Online/offline, last_seen, active runs, version, capabilities.
- Stan degraded przy błędzie self-check.
- UI nie pozwala wystartować na niekompatybilnym runnerze bez ostrzeżenia.

**Kryteria akceptacji**

- Presence aktualizuje się w rozsądnym czasie po rozłączeniu.
- Capabilities są używane w preflight.

**Poza zakresem tej iteracji:** Zaawansowany autoscaling tylko managed runners.

### F4.6. Remote Run Start

**Priorytet:** P0

**Po co:** Uruchomić graph z przeglądarki na wybranym lokalnym runnerze.

**Zakres funkcjonalny**

- Wybór project, graph version, runner i inputs.
- Command jest kolejkujący/idempotentny.
- Runner potwierdza accepted/rejected i tworzy local run id.
- Cloud tworzy powiązany control-plane run record.

**Kryteria akceptacji**

- Podwójne kliknięcie nie tworzy dwóch runów.
- Offline runner nie dostaje fałszywego statusu running.

**Poza zakresem tej iteracji:** Remote shell poza graph runtime jest zabroniony.

### F4.7. Cloud Run Metadata Sync

**Priorytet:** P0

**Po co:** Dać zdalny Run Inspector bez konieczności uploadu repo.

**Zakres funkcjonalny**

- Sync event metadata z konfigurowalnym privacy profile.
- Domyślnie: status, timing, node ids, verdict, attempt count.
- Log/body/artifact content jako opt-in.
- Buffered sync po odzyskaniu połączenia.

**Kryteria akceptacji**

- Run wykonany przy chwilowym offline pojawia się w cloud po reconnect.
- Użytkownik może zobaczyć, jakie kategorie danych są synchronizowane.

**Poza zakresem tej iteracji:** Cloud nie potrzebuje pełnego kodu do podstawowego monitoringu.

### F4.8. Web Human Approval

**Priorytet:** P0

**Po co:** Pozwolić zatwierdzić human gate z innego urządzenia.

**Zakres funkcjonalny**

- Approval request z minimalnym kontekstem i severity.
- Approve/reject z komentarzem.
- Decision nonce/idempotency.
- Runner weryfikuje, że decyzja dotyczy aktywnego gate.

**Kryteria akceptacji**

- Ta sama decyzja nie jest aplikowana dwa razy.
- Stary approval link nie może zatwierdzić innego runa.

**Poza zakresem tej iteracji:** Push/mobile app można dodać później; responsywny web wystarczy.

### F4.9. Cloud Security Baseline

**Priorytet:** P0

**Po co:** Utrzymać model zaufania local-first również po dodaniu chmury.

**Zakres funkcjonalny**

- TLS, encrypted tokens at rest, runner revoke.
- Minimalne scopes API.
- Audit security-sensitive actions.
- Explicit setting dla sync logs/artifacts.
- No remote arbitrary command endpoint.

**Kryteria akceptacji**

- Threat model opisuje runner compromise, account compromise i backend breach.
- Security-sensitive endpointy mają testy authorization.

**Poza zakresem tej iteracji:** Formalne compliance certyfikacje poza alpha.

### F4.10. Optional Artifact Sync

**Priorytet:** P1

**Po co:** Umożliwić wygodny zdalny podgląd diffów/logów dla użytkowników, którzy świadomie włączą upload.

**Zakres funkcjonalny**

- Per-project opt-in.
- Upload wybranych typów artifactów.
- Retention i delete.
- Hash/integrity check.

**Kryteria akceptacji**

- Przy wyłączonym sync żaden artifact body nie opuszcza runnera.

**Poza zakresem tej iteracji:** Domyślnie OFF w pierwszej wersji.

### Exit criteria fazy

> **Faza 4 jest zakończona, gdy:** Użytkownik może sparować laptop jako runner, zobaczyć jego status, wystartować graph z web, obserwować metadata runa i zatwierdzić human gate. Repo może pozostać wyłącznie lokalnie.

## 4.5. Faza 5 - Produkt płatny i współpraca zespołowa

**Target release:** v1.0 - Pro / Team

**Cel fazy:** Monetyzować ciągłość, zdalność, historię i współpracę - nie sztuczne limity lokalnego authoringu. Dodać funkcje potrzebne do codziennego użycia w zespole.

### Zakres feature'ów

| ID | Feature | Priorytet |
|---|---|:---:|
| F5.1 | Plans + Billing | P0 |
| F5.2 | Scheduling | P0 |
| F5.3 | Alerts / Notifications | P0 |
| F5.4 | Long-term History & Analytics | P0 |
| F5.5 | Organizations / Team Workspaces | P0 |
| F5.6 | RBAC | P0 |
| F5.7 | Shared Graph Registry | P0 |
| F5.8 | Shared Constraints / Learning Edge v1 | P1 |
| F5.9 | Policy Engine v1 | P1 |
| F5.10 | Audit Log | P0 |
| F5.11 | Budgets & Guardrails | P1 |
| F5.12 | Git Provider Integration | P1 |

### F5.1. Plans + Billing

**Priorytet:** P0

**Po co:** Wprowadzić jasny upgrade path z Local Free do Pro/Team bez ograniczania podstawowego lokalnego runtime.

**Zakres funkcjonalny**

- Plan entitlements po stronie backendu.
- Subscription status, invoices/provider portal link.
- Grace period i read-only fallback po problemie z płatnością.
- Managed compute rozliczany osobno, jeśli powstanie.

**Kryteria akceptacji**

- Wygaśnięcie subskrypcji nie blokuje dostępu do lokalnych graphów.
- Feature gating jest centralnie testowany.

**Poza zakresem tej iteracji:** Dokładne ceny są hipotezą biznesową, nie częścią kontraktu technicznego.

### F5.2. Scheduling

**Priorytet:** P0

**Po co:** Uruchamiać graphy bez ręcznego klikania, np. nightly audit lub dependency check.

**Zakres funkcjonalny**

- Cron/recurrence, timezone, target runner, inputs.
- Missed-run policy.
- Runner availability handling.
- History schedule executions.

**Kryteria akceptacji**

- Schedule nie uruchamia duplikatów przy retry backendu.
- Offline runner zachowuje czytelny missed/deferred status.

**Poza zakresem tej iteracji:** Sub-minute scheduling nie jest potrzebny.

### F5.3. Alerts / Notifications

**Priorytet:** P0

**Po co:** Informować człowieka tylko wtedy, kiedy potrzebna jest reakcja.

**Zakres funkcjonalny**

- Triggers: human gate, run failed, retries exhausted, runner offline, schedule missed.
- Email + webhook jako pierwsze kanały.
- Per-project preferences i dedup.

**Kryteria akceptacji**

- Jedno zdarzenie nie generuje lawiny identycznych alertów.
- Alert linkuje bezpośrednio do problematycznego run/node.

**Poza zakresem tej iteracji:** Pełny incident management nie jest celem.

### F5.4. Long-term History & Analytics

**Priorytet:** P0

**Po co:** Pokazać jakość i koszt systemu agentowego w czasie, nie tylko pojedynczy run.

**Zakres funkcjonalny**

- Retention zależny od planu.
- Metryki: success rate, time-to-green, retries, human interventions, duration, usage/cost.
- Trend per graph/node.
- Rollback/manual override jako opcjonalny outcome signal.

**Kryteria akceptacji**

- Dashboard potrafi porównać co najmniej dwa okresy.
- Unknown usage nie jest liczone jako zero.

**Poza zakresem tej iteracji:** Zaawansowane BI i custom SQL mogą zostać później.

### F5.5. Organizations / Team Workspaces

**Priorytet:** P0

**Po co:** Współdzielić graphy, runy i runner resources w kontrolowanym kontekście zespołu.

**Zakres funkcjonalny**

- Organization, members, projects.
- Ownership i transfer projectu.
- Invite/remove member.
- Team-visible runners opcjonalnie.

**Kryteria akceptacji**

- Usunięty członek traci dostęp do cloud resources natychmiast.
- Local-only assets użytkownika nie są automatycznie przejmowane przez organizację.

**Poza zakresem tej iteracji:** Enterprise directory sync później.

### F5.6. RBAC

**Priorytet:** P0

**Po co:** Rozdzielić prawo do oglądania, uruchamiania i zatwierdzania agentów.

**Zakres funkcjonalny**

- Role bazowe: Viewer, Operator, Approver, Admin.
- Permissions: view run, start run, cancel, approve gate, edit graph metadata, manage runners.
- Server-side enforcement.

**Kryteria akceptacji**

- Approver bez Operator nie może sam wystartować runa, jeśli polityka tak zakłada.
- UI i API mają spójne uprawnienia.

**Poza zakresem tej iteracji:** Custom roles P1/P2.

### F5.7. Shared Graph Registry

**Priorytet:** P0

**Po co:** Umożliwić zespołowi używanie zatwierdzonych wersji graphów przy zachowaniu Git jako możliwego źródła prawdy.

**Zakres funkcjonalny**

- Published graph versions.
- Draft vs released.
- Changelog i author.
- Pin remote run do konkretnej wersji.

**Kryteria akceptacji**

- Run zawsze wskazuje immutable published version lub repo snapshot.

**Poza zakresem tej iteracji:** Marketplace publiczny poza v1.

### F5.8. Shared Constraints / Learning Edge v1

**Priorytet:** P1

**Po co:** Zamieniać zaakceptowane wnioski w jawne reguły wpływające na przyszłe wykonania.

**Zakres funkcjonalny**

- Constraint ma scope: graph/project/org, author, source run, status proposed/active/retired.
- Human review przed aktywacją domyślną.
- Splitter/planner może konsumować aktywne constraints jako input.
- Każde użycie constraint jest widoczne w trace.

**Kryteria akceptacji**

- Można wskazać, który accepted run doprowadził do powstania reguły.
- Usunięcie/retire reguły nie modyfikuje historycznych runów.

**Poza zakresem tej iteracji:** Automatyczna samomodyfikacja promptów bez review jest poza v1.

### F5.9. Policy Engine v1

**Priorytet:** P1

**Po co:** Wymuszać organizacyjne zasady bezpieczeństwa niezależnie od graph authora.

**Zakres funkcjonalny**

- Policies: path/network permissions, max attempts, required human gate, forbidden node types, required checks.
- Scopes org/project/graph.
- Preflight evaluation + runtime enforcement tam, gdzie potrzebne.
- Policy decision zapisany w audycie.

**Kryteria akceptacji**

- Graph sprzeczny z policy nie może zostać uruchomiony bez jawnego wyjątku uprawnionej osoby.

**Poza zakresem tej iteracji:** Pełny ogólny język policy może być zbyt szeroki; zaczynamy od deklaratywnych reguł domenowych.

### F5.10. Audit Log

**Priorytet:** P0

**Po co:** Odpowiedzieć kto, kiedy i dlaczego uruchomił, zatrzymał lub zatwierdził działanie systemu.

**Zakres funkcjonalny**

- Immutable-style log akcji control-plane.
- Actor, action, target, timestamp, request/run id.
- Events: runner enroll/revoke, run start/cancel, approval, policy change, graph publish, membership change.
- Eksport podstawowy.

**Kryteria akceptacji**

- Każda human approval w Team ma actor identity.
- Admin nie może „edytować” historycznego wpisu.

**Poza zakresem tej iteracji:** WORM/compliance-grade retention później.

### F5.11. Budgets & Guardrails

**Priorytet:** P1

**Po co:** Ograniczać niekontrolowany koszt i długość pracy agentów.

**Zakres funkcjonalny**

- Max attempts już na runtime; dodatkowo max wall time, max concurrent runs, soft usage budget.
- Alert przed limitem i twarde stop policy opcjonalnie.
- Per project/graph.

**Kryteria akceptacji**

- Przekroczenie twardego limitu generuje jednoznaczny terminalny reason.

**Poza zakresem tej iteracji:** Budżet finansowy wymaga wiarygodnych danych od providera; w przeciwnym razie stosować limity techniczne.

### F5.12. Git Provider Integration

**Priorytet:** P1

**Po co:** Połączyć run z issue/PR bez budowania własnego systemu code review.

**Zakres funkcjonalny**

- GitHub jako pierwszy provider: repo identity, issue input, create branch/PR, link run.
- Least-privilege app scopes.
- PR zawiera link do run evidence.

**Kryteria akceptacji**

- Graph może wystartować dla issue i zakończyć utworzeniem PR bez ręcznego kopiowania danych.

**Poza zakresem tej iteracji:** Merge do protected branch pozostaje kontrolowany przez Git provider i human/policies.

### Exit criteria fazy

> **Faza 5 jest zakończona, gdy:** Produkt obsługuje płatne Pro/Team, harmonogramy i alerty, wspólną historię, organizacje, RBAC i audit. Zespół może zarządzać agentami jak systemem operacyjnym, a nie zestawem prywatnych terminali.

## 4.6. Faza 6 - Po v1: managed execution i enterprise hardening

**Target release:** Post-v1

**Cel fazy:** Dopiero po potwierdzeniu popytu na control plane dołożyć kosztowną warstwę wykonywania nieufnego kodu klientów i funkcje enterprise.

### Zakres feature'ów

| ID | Feature | Priorytet |
|---|---|:---:|
| F6.1 | Managed Ephemeral Runners | P2 |
| F6.2 | Managed Artifact Storage | P2 |
| F6.3 | Enterprise Identity | P2 |
| F6.4 | Advanced Policy / Approval Chains | P2 |
| F6.5 | Plugin/Adapter SDK | P2 |

### F6.1. Managed Ephemeral Runners

**Priorytet:** P2

**Po co:** Uruchamiać graph bez zależności od komputera użytkownika.

**Zakres funkcjonalny**

- Ephemeral VM/container per run lub izolowany worker.
- Clone repo na konkretnym commit.
- Short-lived credentials.
- Destroy workspace po zakończeniu.
- Resource classes i metering.

**Kryteria akceptacji**

- Brak cross-tenant workspace reuse.
- Worker jest usuwany po terminalnym stanie.

**Poza zakresem tej iteracji:** Nie budować przed działającym local/hybrid control plane.

### F6.2. Managed Artifact Storage

**Priorytet:** P2

**Po co:** Przechowywać logs/diffs/reports dla managed runs i zespołów, które świadomie chcą pełnego cloud trace.

**Zakres funkcjonalny**

- Encryption at rest, retention, delete, per-org access.
- Signed temporary downloads.
- Region strategy później.

**Kryteria akceptacji**

- Artifact access podlega RBAC i audytowi.

**Poza zakresem tej iteracji:** Nie jest wymagane dla local-first MVP.

### F6.3. Enterprise Identity

**Priorytet:** P2

**Po co:** Dostosować control plane do większych organizacji.

**Zakres funkcjonalny**

- SSO/SAML/OIDC enterprise.
- SCIM/directory sync w dalszym kroku.
- Domain verification.

**Kryteria akceptacji**

- Deprovisioned user traci dostęp zgodnie z identity provider.

**Poza zakresem tej iteracji:** Nie wpływa na core execution semantics.

### F6.4. Advanced Policy / Approval Chains

**Priorytet:** P2

**Po co:** Obsłużyć wysokiego ryzyka operacje wymagające kilku niezależnych kontroli.

**Zakres funkcjonalny**

- Multiple approvers, separation of duties, environment-specific policies.
- Closed lanes dla nieodwracalnych działań.
- Approval expiry.

**Kryteria akceptacji**

- Operacja z wymaganiem dwóch approverów nie może przejść po jednej decyzji.

**Poza zakresem tej iteracji:** Nie zastępować zewnętrznych systemów change management, jeśli integracja wystarcza.

### F6.5. Plugin/Adapter SDK

**Priorytet:** P2

**Po co:** Pozwolić integrować nowe agenty i narzędzia bez zmian w core.

**Zakres funkcjonalny**

- Versioned adapter protocol.
- Capability manifest.
- Conformance test kit.
- Local plugin loading z allowlist.

**Kryteria akceptacji**

- Przykładowy third-party adapter przechodzi conformance tests.

**Poza zakresem tej iteracji:** Marketplace i podpisywanie pluginów jako osobny etap.

### Exit criteria fazy

> **Faza 6 jest zakończona, gdy:** Managed execution jest izolowane i mierzalne, a enterprise funkcje nie komplikują local-first core dla pojedynczego developera.

## 5. Cross-cutting requirements

Te wymagania powinny być sprawdzane w każdej fazie, nawet jeśli nie występują jako osobny feature.

### 5.1. Backward compatibility schematów

- Każdy graph i event ma jawne `schema_version`.
- Breaking changes wymagają migratora lub jednoznacznego błędu z instrukcją migracji.
- Runtime nie powinien po cichu reinterpretować starego graphu według nowych zasad.

### 5.2. Determinizm tam, gdzie jest możliwy

- Scheduling, binding danych, gate oparte na commandach, scope checks i policy decisions powinny być deterministyczne.
- Niedeterministyczność modelu pozostaje zamknięta w Agent node i jest widoczna przez attempts/trace.
- Nie używać LLM do transformacji, które mają jedną mechaniczną odpowiedź.

### 5.3. Idempotency

- Remote commands, approvals i scheduler muszą mieć idempotency keys.
- Retry transportu nie może oznaczać retry pracy biznesowej.
- Side effects mają być oznaczane i uwzględniane w recovery.

### 5.4. Security defaults

- Local runner nie otwiera publicznego portu.
- Cloud nie ma endpointu „wykonaj dowolną komendę”; wykonuje wyłącznie zdefiniowany graph/run command.
- Sekrety są referencjami i nie trafiają do logów/eksportów.
- Permissions są jawne i możliwe do audytu.

### 5.5. Observability defaults

- Każdy state transition emituje event.
- Każdy red verdict ma reason i evidence.
- Każdy human decision ma actor/decision/timestamp w trybie Team.
- Unknown metrics pozostają unknown; nie są zamieniane na zero.

## 6. Ekrany produktu i moment, w którym powstają

| Surface | Pierwsza faza | Minimalna zawartość |
|---|:---:|---|
| CLI run/validate | 0 | start runa, statusy node'ów, exit code, JSON output |
| Graph Canvas | 2 | DAG, typy node'ów, porty, status live |
| Node Inspector | 2 | inputs, execution, loops/checks, outputs, permissions |
| Run Inspector | 2 | timeline, attempts, reason/evidence, artifacts |
| Local History | 2 | run list, filters, graph/repo metadata |
| Human Approval | 2 local / 4 web | blast radius, checks, diff, approve/reject |
| Runner Fleet | 4 | online/offline, capabilities, active runs |
| Analytics | 5 | success, time-to-green, retries, human rate, cost |
| Organization/Admin | 5 | members, roles, audit, policies |

## 7. Minimalny backlog pod pracę licencjacką

Jeżeli głównym celem równoległym jest praca **„Wdrażanie systemów agentowych AI do środowisk produkcyjnych”**, najbardziej wartościowy zakres badawczo-inżynierski to fazy 0-3 z niewielkim fragmentem fazy 4. Nie trzeba budować billing, RBAC ani managed compute, aby wykazać problem produkcyjności agentów.

**Zakres rekomendowany do thesis MVP:**

1. F0.1-F0.7 - graph, scheduler, event model i CLI.
2. F1.1-F1.7 - agent adapter, gate, check contract i bounded correction loop.
3. F2.5-F2.10 - live execution, run inspector, history i human gate; Graph Canvas może być prostszy niż docelowo.
4. F3.1-F3.9 - izolacja, recovery, artifacts, reproducibility, secrets i execution policy.
5. Opcjonalnie F4.3-F4.8 - jako demonstracja modelu hybrid deployment i remote approval.

**Najmniej istotne dla licencjatu, ale istotne komercyjnie:** billing, organizacje, RBAC, długoterminowe analytics, enterprise identity i managed runners.

## 8. Metryki produktu i jakości systemu

| Metryka | Znaczenie | Od której fazy |
|---|---|:---:|
| Run success rate | odsetek runów zakończonych zaakceptowanym wynikiem | 1 |
| Time-to-green | czas od startu do przejścia wymaganych gate'ów | 1 |
| Attempts per accepted unit | ile korekt potrzeba do green | 1 |
| Local retry ratio | czy retry pozostaje ograniczony do błędnego unit | 1 |
| Parallelism saved | różnica między sumą czasów node'ów a wall-clock | 2 |
| Human intervention rate | ile runów wymaga decyzji człowieka | 2 |
| Recovery success | procent przerwanych runów poprawnie wznowionych | 3 |
| Policy violation rate | liczba blokowanych naruszeń scope/permissions | 3 |
| Remote start reliability | skuteczne zdalne starty bez duplikatów | 4 |
| Approval latency | czas oczekiwania na human gate | 4 |
| Cost per accepted result | koszt providera na zaakceptowany rezultat | 3/5 |
| Rollback/manual override rate | sygnał jakości po „green” | 5 |

## 9. Decyzje „nie budować jeszcze”

- Nie budować własnego modelu/LLM ani nowego frameworka promptowego.
- Nie budować managed cloud compute przed potwierdzeniem, że ludzie chcą control plane i local runner.
- Nie budować publicznego marketplace graphów przed ustabilizowaniem schematu i adapter SDK.
- Nie budować Kubernetes-native orchestration jako wymagania core.
- Nie budować „agent memory” jako niejawnego wektorowego worka z kontekstem; learning edge powinien tworzyć jawne constraints.
- Nie budować złożonego ogólnego policy language przed poznaniem rzeczywistych reguł używanych przez zespoły.
- Nie dodawać wielu providerów przed ustabilizowaniem Agent Adapter Contract na co najmniej jednym realnym workflow.

## 10. Proponowany porządek implementacji wewnątrz repo

```text
packages/
  graph-schema/        # schema, parser, validator, migrations
  execution-core/      # scheduler, state machine, retry semantics
  event-model/         # event envelopes i serialization
  adapters/            # shell + agent provider adapters
  workspace-manager/   # git/worktrees, artifacts, permissions
  local-store/         # SQLite + artifact index
  runner-daemon/       # process supervision, API/IPC, cloud connection
  cli/                 # developer CLI
  ui/                  # graph editor + run inspector
  cloud-api/           # account/project/runner/control plane
```

Granice package'ów powinny odpowiadać granicom domenowym. W szczególności UI nie powinno implementować semantyki schedulera, a cloud API nie powinno umieć wykonywać dowolnego kodu poza protokołem runnera.

## 11. Definition of Done dla feature'u

Feature można uznać za ukończony dopiero, gdy spełnia wszystkie punkty właściwe dla jego warstwy:

- ma testy jednostkowe dla głównej semantyki;
- ma test end-to-end, jeśli dotyka execution path;
- emituje potrzebne eventy i nie wymaga parsowania przypadkowego tekstu w UI;
- ma czytelny failure mode i reason;
- ma jawny wpływ na security/privacy;
- nie zapisuje sekretów w trace;
- ma dokumentację konfiguracji;
- jeśli ma UI: działa po odświeżeniu na podstawie stanu backend/runtime, nie tylko lokalnego stanu komponentu;
- jeśli ma side effect: jego zachowanie podczas retry/recovery jest opisane;
- ma co najmniej jedną metrykę lub event pozwalający ocenić, czy działa w rzeczywistych runach.

## 12. Milestones demonstracyjne

### Demo A - „Graph naprawdę ma znaczenie”
Dwie niezależne gałęzie wykonują się równolegle; UI/CLI pokazuje oszczędność wall-clock. Ten milestone waliduje model edge jako dependency danych.

### Demo B - „Loop naprawia unit, nie batch”
Agent wprowadza błąd, test go odrzuca, return envelope wskazuje reason/evidence/scope, a drugi attempt poprawia wyłącznie dany unit. Inne accepted node'y nie są uruchamiane ponownie.

### Demo C - „Debugger zamiast terminali”
Run Inspector pokazuje input, output, diff, check, attempts i human gate. Użytkownik potrafi wyjaśnić failure bez przeszukiwania surowych logów.

### Demo D - „Produkcyjność lokalna”
W połowie runa proces runnera zostaje zabity. Po restarcie accepted work nie jest powtarzany, niepewny side effect wymaga decyzji, a trace zachowuje historię sprzed awarii.

### Demo E - „Cloud nie musi mieć repo”
Web control plane startuje graph na lokalnym runnerze, obserwuje metadata i obsługuje approval; kod repo i artifacts pozostają lokalne przy wyłączonym sync.

## 13. Kryterium gotowości do v1.0

v1.0 nie powinno być definiowane liczbą feature'ów, tylko zdolnością systemu do bezpiecznego i zrozumiałego wykonywania agentów w codziennym workflow. Minimalnie:

1. Graph execution jest stabilne, wersjonowane i możliwe do wznowienia.
2. Gate/loop/retry mają jawne, testowalne semantyki.
3. Run Inspector pozwala wyjaśnić każdy czerwony verdict i każdą decyzję człowieka.
4. Local Free jest realnie użyteczny bez konta cloud.
5. Remote control działa przez outbound runner connection i nie tworzy backdoora do dowolnego shell execution.
6. Team ma minimalne RBAC i audit dla start/cancel/approve/publish.
7. Niezaakceptowany attempt nie może przypadkowo „przeciec” do kolejnych node'ów lub głównego workspace.
8. Sekrety i privacy profile mają bezpieczne defaulty.
9. Zespół potrafi zmierzyć time-to-green, retry rate i human intervention rate.
10. Managed compute pozostaje opcjonalne i nie jest warunkiem działania produktu.