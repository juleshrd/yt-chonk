# Implémentation V1

## Model selection and pricing in setup

- [x] Verify current file-transcription models and official USD pricing.
- [x] Add an explicit model choice with per-minute and per-hour prices; preserve saved choices and CLI overrides.
- [x] Cover selection, repeat setup, invalid input and cancellation; update short English docs.
- [ ] Run Go checks and four CGO-free builds, then publish an updated Homebrew revision.

Plan: use an offline, dated price snapshot for the two supported file models.
Keep Mini 3 as the default; explain the local backend has no API fee.
Show the official pricing link and do not make a paid/network call in setup.

### Model setup review

- Official Mistral pricing and file-transcription docs verified on 2026-11-11.
  Mini 3: USD 0.0035/min (0.21/hour); Small 3: USD 0.004/min (0.24/hour).
  Deprecated Mini 2 and the WebSocket realtime model are excluded.
- Setup saves the choice, keeps it on repeat setup, handles a backend switch,
  and retains custom configured IDs with an explicit unknown-price label.
  A bad selection leaves the existing config untouched. CLI overrides still
  select their own cache without changing the saved choice.
- gofmt, go test -race ./..., go vet ./... and git diff --check passed.
  Existing cancellation test passed. Test servers are local; no paid API calls.
- Version 0.1.1 built with CGO disabled for darwin/amd64, darwin/arm64,
  linux/amd64 and linux/arm64. Archive checksums verified. macOS arm64 binary
  setup exercised with temporary config (Small selected), and version checked.
  README/SPEC/integration local links checked. Independent review found no issues.
- Full Homebrew installation and real Mistral transcription remain untested.

Plan validé par l'utilisateur le 11 novembre 2026.

- [x] Modèle commun et adaptateurs externes (processus, YouTube, audio, moteurs).
- [x] Cache versionné, verrouillage, reprise et exports atomiques.
- [x] CLI, configuration, setup, doctor et orchestration.
- [x] Tests automatisés, vet et builds sans CGO sur quatre cibles.
- [x] Documentation, CI, archives et revue des limites réellement vérifiées.

## Revue

Vérifications exécutées sur macOS arm64, avec Go 1.23.12 téléchargé depuis
la distribution officielle dans `/private/tmp/yt-chonk-sdk` :

- `gofmt` appliqué ; contrôle final sans fichier à reformater.
- `go test -race ./...` réussi. Les serveurs HTTP locaux ont nécessité une
  exécution autorisée hors sandbox ; aucun service Mistral n'a été contacté.
- `go vet ./...` réussi.
- Binaire racine compilé avec `CGO_ENABLED=0` ; aide et version exécutées.
- Script release exécuté : archives darwin/amd64, darwin/arm64, linux/amd64,
  linux/arm64. Les quatre sommes SHA-256 ont été vérifiées et le contenu
  de l'archive macOS arm64 a été inspecté.
- Schéma JSON et fixtures validés avec jsonschema ; liens documentaires locaux
  vérifiés. `git diff --check` réussi (les nouveaux fichiers restent non suivis).

Tests simulés : requêtes multipart Mistral, erreurs permanentes et temporaires,
Retry-After, borne de quatre essais, réponses invalides, silence, JSON whisper,
annulation des processus descendants, durées limites, publication exclusive,
verrous après crash et nettoyage concurrent. Le test CLI reprend après échec au
second morceau sans retranscrire le premier, assemble les temps globaux, puis
réexporte sans outils, modèle ni clé. Autres régressions couvertes : crash avant
assemblage final, modèle remplacé au même chemin, langues contradictoires,
annulation de setup et configuration initiale dans un dossier absent.

## Limites et choix documentés

- Aucun téléchargement YouTube réel, appel payant Mistral ou essai d'inférence
  whisper.cpp réel n'a été effectué. Aucun essai d'exécution Linux ni macOS Intel
  n'a été effectué. La CI macOS/Linux est ajoutée mais n'a pas été exécutée ici.
- Les intégrations et leurs sources primaires sont documentées dans
  `docs/INTEGRATIONS.md`. Le découpage de 300 secondes est un choix applicatif.
- `setup` guide l'installation et le téléchargement explicite du modèle ; il
  n'exécute pas de gestionnaire de paquets ou de téléchargement de modèle.
- Le trousseau utilise `security -i` sur macOS et `secret-tool` sur Linux ;
  aucune clé réelle n'a été enregistrée pour tester cette intégration. La saisie
  est masquée et le secret passe par stdin, jamais dans les arguments.
- `doctor` ne contacte pas YouTube et ne certifie pas le fonctionnement d'EJS
  ni l'accélération Metal ; il donne les chemins et diagnostics disponibles.
- Le cache local vérifie l'empreinte du modèle s'il est encore disponible. Son
  remplacement au même chemin demande `cache clean` ; son absence n'empêche
  pas le réexport d'un résultat terminé.
- Le binaire et les archives dans `dist/` sont générés et ignorés par Git.
  Aucune publication externe, release GitHub ou modification du trousseau réel.

## English CLI and short README

- [x] Translate CLI help, prompts, diagnostics and Markdown metadata to English.
- [x] Write a short English README with installation into PATH and usage examples.
- [x] Align the product language in the specification.
- [x] Run formatting, tests, vet and builds; refresh local binary and archives.

### Review

- CLI help, setup prompts (including yes/no), progress, errors, Markdown labels
  and release messages are now English. Transcribed content is unchanged.
- README reduced to 76 lines with build, installation into ~/.local/bin,
  persistent PATH setup, dependencies and usage. Product language updated
  in docs/SPEC.md; correction recorded in tasks/lessons.md.
- gofmt, go test ./... and go vet ./... passed. Existing error assertions
  updated; Markdown labels checked in the existing export test.
- Root binary rebuilt with CGO disabled; main and transcribe help inspected.
  Four target archives regenerated. README local links and git diff --check passed.
- No installation into the user's home directory was performed. README and
  response provide the requested commands. No real transcription was run.

## Simplify installation with Homebrew

- [x] Add a formula in this repository, with an immutable source revision and all runtime dependencies.
- [x] Replace manual installation in the README with tap/install commands.
- [x] Validate formula loading, build/test commands and local links.
- [x] Prepare the verified formula and README for commit and publication.

### Homebrew verification

- ruby -c passed. The formula loaded successfully with Homebrew 7.0.13;
  source revision, version and dependency metadata verified.
- Underlying Go build with CGO disabled and the formula version flag passed;
  generated binary --version and --help ran successfully.
- README local links and git diff --check passed. No application Go code changed.
- Full brew install / brew test not performed: no changes were made to the
  host's packages or taps. whisper.cpp model download remains explicit.
- The formula pins the existing published app commit; future app releases
  must bump the formula version and source revision together.
