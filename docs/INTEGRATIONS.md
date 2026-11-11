# Intégrations V1

État documentaire vérifié le 11 novembre 2026. Les sources ci-dessous décrivent
les contrats utilisés ; leur consultation ne constitue pas un essai réel du
service, de YouTube ou d'une inférence locale. Voir la revue de
[tasks/todo.md](../tasks/todo.md) pour les vérifications exécutées.

## Audio et découpage

Le pipeline prépare du WAV mono PCM 16 bits à 16 kHz et traite des morceaux
contigus de 300 secondes, sans recouvrement. Un morceau de cinq minutes contient
9 600 000 octets de données PCM, auxquels s'ajoute l'en-tête WAV. Ce choix borne
la taille des requêtes et le travail à refaire après interruption. Les paramètres
font partie de la compatibilité du cache. Les opérations FFmpeg sont invoquées
avec des arguments distincts, sans shell ; les horodatages de chaque morceau
sont décalés lors de l'assemblage.

## Mistral

Le [guide de transcription](https://docs.mistral.ai/studio/audio/speech-to-text/transcribe-files)
documente l'envoi multipart à `/v1/audio/transcriptions`, le modèle
`voxtral-mini-transcribe-3`, la langue optionnelle et la demande de timestamps
par segment. L'adaptateur utilise ces champs, désactive le streaming de réponse
et normalise le JSON reçu. `return_language=true` est envoyé en détection
automatique ; une langue inconnue reste nulle. Les timestamps au mot et les
numéros de locuteur ne sont pas exposés par le CLI.

La page [limitations connues](https://docs.mistral.ai/resources/known-limitations)
annonce WAV parmi les formats acceptés, au maximum 60 minutes et 500 MB par
fichier de transcription. Les morceaux de cinq minutes, d'environ 9,6 MB de PCM,
restent en deçà de ces bornes. La page spécialisée
[limites audio](https://docs.mistral.ai/studio/audio/specs-and-limits/limits)
mentionne également la fenêtre de contexte ; sa table ne fournit pas de valeur
lisible de taille de fichier lors de cette vérification. Ces pages peuvent
évoluer ; le découpage n'est pas une garantie de capacité pour tout modèle
personnalisé fourni avec `--model`.

Aucun appel payant n'a été utilisé pour valider ces hypothèses. Les tests HTTP
locaux vérifient la forme de la requête, les réponses, l'annulation et la borne
de nouvelles tentatives. Les corps d'erreur du service ne sont pas imprimés,
afin de ne pas exposer de secrets ou de transcription. Une requête interrompue
peut avoir été facturée avant l'échec local.

## whisper.cpp

Le CLI est externe : aucune liaison C++ et aucun téléchargement implicite
pendant une transcription. Les options utilisées sont `-m`, `-f`, `-l`, `-oj`,
`-of` et `-np`. Le [code du CLI officiel](https://github.com/ggml-org/whisper.cpp/blob/master/examples/cli/cli.cpp)
écrit `result.language` et une liste `transcription` avec `text` et `offsets`.
Les valeurs `offsets.from` et `offsets.to` sont en millisecondes ; l'adaptateur
les convertit en secondes. Une liste vide est un résultat valide, distinct
d'une réponse JSON où `transcription` serait absent.

Les tests utilisent un faux exécutable produisant cette forme de JSON. Ils ne
mesurent ni la qualité, ni la mémoire, ni les performances CPU/Metal. Le modèle
initial recommandé est `small` multilingue ; les modèles restent séparés du
binaire applicatif. Les capacités d'accélération dépendent du whisper.cpp installé.

## yt-dlp et JavaScript

L'adaptateur utilise `--ignore-config` et `--no-playlist` et passe une URL
canonique unique après `--`. Il récupère les métadonnées avant l'audio et vérifie
la durée et l'état du direct. Une configuration yt-dlp personnelle ne peut donc
pas ajouter implicitement cookies, sorties ou options au traitement.

Le [guide EJS](https://github.com/yt-dlp/yt-dlp/wiki/EJS) distingue le runtime
JavaScript des scripts de résolution. Deno est le runtime retenu par la V1 ;
les scripts doivent être fournis par une distribution yt-dlp compatible.
`doctor` inspecte les exécutables sans contacter YouTube ; il ne certifie pas
la résolution effective des challenges ni l'accès à une vidéo donnée.

## Format JSON et évolution

[JSON-SCHEMA.json](JSON-SCHEMA.json) décrit `schema_version: 1`.
Les [fixtures](fixtures/transcript.json) sont synthétiques et ne proviennent
pas d'une vidéo réelle ; [silence.json](fixtures/silence.json) illustre une
transcription vide et les métadonnées nulles. Les temps sont globaux, en secondes.
En plus du schéma, l'application doit vérifier `start <= end`, l'ordre des
segments et leur inclusion dans la durée audio réelle.

Toute modification incompatible de cette structure doit changer
`schema_version` et fournir des fixtures distinctes. Une liste `segments: []`
ne signifie pas que des timestamps ont été fournis. Les exports TXT et Markdown
consomment la même représentation et n'ajoutent aucun appel réseau.
