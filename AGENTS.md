# yt-chonk — instructions pour les agents

## Mission et contrat produit

Construire un CLI Go qui transcrit l'audio d'une vidéo YouTube avec Mistral
Voxtral ou whisper.cpp local. Le choix du moteur appartient à l'utilisateur.
La priorité est une distribution simple sur macOS et Linux, avec le calcul
intensif confié à FFmpeg et whisper.cpp.

Avant de concevoir une commande, modifier le pipeline, un moteur, le cache ou un
export, lire [docs/SPEC.md](docs/SPEC.md). Ce document est la source de vérité
du périmètre V1, des comportements attendus et des critères d'acceptation.
Il décrit une cible : vérifier dans le code ce qui est réellement implémenté.

## Architecture

- Garder le CLI en Go ; communiquer avec Mistral par son API HTTP documentée.
- Exécuter yt-dlp, FFmpeg/ffprobe et `whisper-cli` comme processus externes.
  Conserver whisper.cpp externe pour simplifier les builds et bénéficier de
  ses accélérations natives. Le CLI ne requiert pas de bindings C++.
- Séparer orchestration, adaptateurs externes, cache et sérialisation. Garder
  les règles de reprise et d'export indépendantes des commandes terminal.
- Définir une représentation commune de la transcription et de ses segments.
  Normaliser les réponses des moteurs à cette frontière.
- Introduire des interfaces aux frontières nécessitant une substitution pour
  les tests ; privilégier des fonctions et structures concrètes ailleurs.
- Propager `context.Context` aux requêtes et sous-processus, borner les appels
  et traiter l'annulation jusqu'aux processus enfants.
- Utiliser des arguments structurés avec `os/exec`, plutôt qu'une commande
  construite pour un shell. Traiter URL, titres et transcriptions comme des données.
- Traiter l'audio par fichiers et segments ; borner mémoire et concurrence.
  Réaliser d'abord un pipeline séquentiel correct et reprenable.

## Invariants

- Transcription fidèle dans la langue originale ; résumé, traduction et
  réécriture par LLM restent hors V1.
- Un moteur explicitement choisi, sans bascule automatique entre local et cloud.
  Le mode local garde audio et transcription sur la machine ; l'accès à YouTube
  et le téléchargement initial des modèles restent des opérations réseau.
- Clés API via `MISTRAL_API_KEY` ou trousseau système. Conserver les secrets hors
  des arguments de processus, fichiers de configuration, caches et journaux.
- Les segments terminés constituent le point de reprise. Une sortie existante
  est remplacée uniquement avec `--force`.
- Une réussite correspond à un export complet écrit atomiquement. Un résultat
  partiel reste dans le cache et ne porte pas le nom du résultat final.

## Workflow de contribution

1. Lire les instructions applicables, `tasks/lessons.md` et les fichiers
   concernés. Pour une tâche non triviale, écrire un plan vérifiable avec cases
   à cocher dans `tasks/todo.md` et l'exposer avant l'implémentation.
2. Déléguer les recherches ou relectures indépendantes à des sous-agents lorsque
   cela aide. Donner une tâche précise à chacun et vérifier leurs conclusions.
3. Faire le changement minimal qui résout la cause. En cas de contradiction ou
   d'échec invalidant le plan, revoir le plan avant de poursuivre.
4. Mettre à jour l'avancement et expliquer les décisions qui affectent le
   comportement. L'autorisation déjà donnée suffit pour le travail couvert.
5. Vérifier le résultat, puis consigner les contrôles réellement effectués et
   leurs limites dans la revue de `tasks/todo.md`.
6. Après une correction utilisateur, ajouter la règle générale utile dans
   `tasks/lessons.md` et corriger la source de vérité concernée.

## Vérification

Pour une modification Go, une fois le module initialisé : appliquer `gofmt`,
exécuter `go test ./...`, `go vet ./...` et compiler les cibles concernées.
Pour une modification documentaire, vérifier cohérence, liens locaux et diff.

Tester les comportements à risque : contrats CLI, erreurs des moteurs,
annulation, reprise, absence d'écrasement et assemblage des horodatages.
Utiliser des serveurs HTTP de test et de faux exécutables pour garder les tests
automatiques sans clé API, sans paiement et sans téléchargement YouTube.
Les essais réels doivent être explicitement distingués des tests simulés.

Pour les releases, vérifier des builds `CGO_ENABLED=0` pour
`darwin/amd64`, `darwin/arm64`, `linux/amd64` et `linux/arm64`.
La compilation croisée ne remplace pas un essai sur le système cible.

Avant de changer l'intégration Mistral, yt-dlp ou whisper.cpp, consulter les
sources primaires liées dans la spécification : modèles, limites et formats
évoluent. Documenter les capacités effectivement vérifiées.
