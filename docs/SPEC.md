# yt-chonk — contrat V1

Ce cadrage a été validé par entretien avec l'utilisateur. Les conventions
détaillées ci-dessous rendent les décisions exploitables ; elles décrivent
le comportement cible et ne signifient pas que le CLI existe déjà.

## Périmètre validé

| Sujet | Décision |
| --- | --- |
| Application | CLI Go, macOS et Linux, distribuable à d'autres utilisateurs |
| Entrée | Une URL de vidéo YouTube par commande |
| Source | Audio téléchargé puis transcrit, même si des sous-titres existent |
| Moteurs | API Mistral Voxtral ou whisper.cpp local, choix explicite mémorisable |
| Contenu | Transcription fidèle, ponctuation et paragraphes sans réécriture du sens |
| Langue | Détection automatique ou langue imposée ; sortie dans la langue originale |
| Durée | Vidéos jusqu'à quatre heures incluses |
| Exports | TXT par défaut ; Markdown et JSON au choix |
| Reprise | Cache local de l'audio et des segments terminés après interruption |
| Accès YouTube | Vidéos accessibles sans connexion, publiques ou non répertoriées |
| Interface | Aide, messages et README en anglais ; commandes, options et code en anglais |

Hors V1 : playlists et lots, chaînes, fichiers audio fournis directement,
vidéos privées ou authentifiées, cookies, directs en cours, récupération de
sous-titres YouTube, SRT/VTT, résumé, traduction, réécriture par LLM et interface
graphique. Les numéros de locuteur et horodatages au mot ne sont pas des promesses V1.

## Contrat CLI

Surface cible :

```text
yt-chonk setup
yt-chonk doctor
yt-chonk transcribe URL [--backend mistral|local] [--format txt|md|json]
                        [--language CODE] [--model MODEL]
                        [--output PATH] [--force] [--quiet]
yt-chonk cache clean
```

- `setup` mémorise le moteur et les préférences ; il peut configurer la clé
  Mistral via une saisie masquée. Il guide l'installation des dépendances et
  modèles. Les téléchargements sont des actions explicites, jamais un effet
  caché d'une simple consultation d'aide ou d'un diagnostic.
- `doctor` vérifie uniquement les capacités utiles au moteur choisi : outils,
  versions, chemins, modèle local et disponibilité du moyen d'authentification.
  Il explique comment résoudre un manque. Il ne lance pas de transcription payante.
- Priorité de configuration : options CLI, configuration persistée, valeurs
  par défaut documentées. Sans moteur configuré ou fourni, demander de lancer
  `setup` ou de fournir `--backend` ; la présence d'une clé ne sélectionne pas le cloud.
- `--model` désigne un modèle du moteur retenu. Le modèle local initial est
  Whisper `small` multilingue, modifiable ; ses fichiers sont séparés du binaire.
- `--language` absent signifie détection automatique. Une langue imposée
  s'applique à tous les segments. Conserver une détection cohérente à l'échelle
  de la vidéo et enregistrer les informations disponibles sans inventer de certitude.
- `--format` sélectionne un seul export ; défaut `txt`. `--output` désigne le
  fichier final. Sans cette option : dossier courant, titre nettoyé et identifiant
  YouTube, avec extension adaptée. L'identifiant sert de repli si le titre est vide.
- Vérifier les collisions avant de lancer la transcription, puis garantir
  l'absence d'écrasement à la publication du fichier. `--force` autorise le
  remplacement de l'export et n'impose pas de retranscription.
- Afficher progression et diagnostics sur `stderr`. Sur réussite, `stdout`
  contient uniquement le chemin du fichier final. `--quiet` supprime la
  progression et les messages informatifs, en conservant les erreurs.
- Garder un fonctionnement non interactif pour `transcribe` ; réserver les
  questions de configuration à `setup`. Les erreurs indiquent une action utile.
- Codes de sortie : `0` réussite, `1` échec opérationnel, `2` usage ou configuration
  invalide, `130` interruption utilisateur. Un traitement partiel est un échec.

## Pipeline et frontières

1. Valider l'URL YouTube et les options. Identifier une vidéo unique, sans
   développer de playlist, même si l'URL contient un paramètre de playlist.
2. Consulter le cache compatible pour déterminer les étapes restantes. Un
   réexport de transcription complète utilise ses métadonnées persistées, sans
   exiger de clé API, de modèle local, d'outil audio ni de requête YouTube.
3. Vérifier la configuration et les dépendances des seules étapes nécessaires.
   Si l'acquisition reste nécessaire, obtenir les métadonnées avec yt-dlp :
   identifiant, URL canonique, titre, chaîne et durée. Rejeter les directs en
   cours et les durées supérieures à quatre heures avant l'envoi audio payant.
   Vérifier aussi la durée audio réelle avant une nouvelle transcription.
4. Télécharger l'audio puis le préparer avec FFmpeg/ffprobe selon les formats
   compatibles avec le moteur. Préférer le disque et le streaming aux gros
   tampons mémoire ; libérer les ressources mémoire au fil du traitement et
   conserver l'audio sur disque jusqu'à la réussite de l'export.
5. Découper l'audio avec une stratégie compatible avec les limites effectives
   du moteur, puis transcrire les segments manquants. Garder les limites et
   paramètres testables ; ne pas figer une durée de segment à partir d'une
   hypothèse non vérifiée sur les limites de Voxtral.
6. Normaliser les résultats, appliquer les décalages temporels globaux et
   assembler le texte dans l'ordre. Si une fenêtre de recouvrement est utilisée,
   traiter les frontières sans supprimer les répétitions réellement prononcées.
7. Écrire l'export complet atomiquement, puis supprimer l'audio du traitement.

La frontière moteur reçoit un segment audio, la langue et les paramètres du
modèle ; elle renvoie texte, segments disponibles et métadonnées effectives.
L'orchestrateur possède le découpage, les tentatives, la persistance et la reprise.
Les exporteurs consomment la même représentation normalisée, sans appels réseau.
Ces responsabilités peuvent rester dans peu de packages ; leur séparation
n'impose ni framework, ni système de plugins.

## Moteurs et dépendances

### Mistral

- Utiliser l'API HTTP de transcription Voxtral et garder l'identifiant du
  modèle configurable. Vérifier la documentation au moment de l'intégration
  avant de fixer la valeur par défaut et les limites acceptées.
- La transcription est directe, sans passage par un LLM texte.
- Vérifier formats audio, contexte, langue et granularité des timestamps avec
  le modèle effectivement choisi. La V1 vise les segments, sans garantie au mot.
- Protéger les requêtes par délais d'expiration et respecter `Retry-After`
  lorsqu'il est applicable. Éviter de réexécuter une requête de création simplement
  parce qu'une consultation de son résultat a échoué, si l'API permet cette distinction.

### Local

- Invoquer `whisper-cli` de whisper.cpp. Le CPU constitue la base de compatibilité ;
  utiliser Metal sur les Mac compatibles quand le moteur installé le permet.
- `doctor` indique le modèle, le chemin et les capacités détectées. Documenter
  le compromis mémoire/vitesse/qualité sans promettre de temps réel sur CPU.
- Les opérations de transcription locales gardent l'audio et le texte sur la
  machine. Une erreur locale ne déclenche jamais d'envoi à Mistral.

### Distribution

Distribuer le binaire Go par système et architecture, avec archives, sommes de
contrôle et instructions d'installation. Garder le CLI compilable sans CGO.
Une installation simplifiée, notamment via Homebrew, peut être ajoutée sans
changer les contrats fonctionnels.

Le binaire applicatif ne remplace pas yt-dlp, FFmpeg/ffprobe, le runtime JavaScript
nécessaire au support YouTube, ni whisper.cpp et ses modèles pour le local.
Privilégier les distributions officielles compatibles : certains exécutables
yt-dlp embarquent Python, d'autres le requièrent séparément. Vérifier aussi la
disponibilité d'EJS et du runtime JS ; Deno est actuellement recommandé par yt-dlp.
Le téléchargement de modèles locaux n'est pas nécessaire au mode Mistral.

## Configuration, cache et erreurs

- Employer les dossiers de configuration et de cache conventionnels de l'OS.
  Versionner le format de configuration et écrire les mises à jour atomiquement.
- Clé Mistral : variable `MISTRAL_API_KEY` prioritaire, puis trousseau système.
  Si le trousseau est indisponible, expliquer l'usage de la variable ; garder
  la configuration persistée sans secret. Masquer les secrets dans les diagnostics,
  y compris les erreurs HTTP.
- Isoler chaque traitement avec un manifeste versionné : identifiant vidéo,
  empreinte de l'audio préparé, moteur, modèle, langue, options pertinentes,
  stratégie de découpage et état des segments. Vérifier la compatibilité avant
  toute réutilisation. Un changement de format d'export seul peut réutiliser la transcription.
- Persister atomiquement chaque segment terminé. La relance de la même commande
  reprend les étapes compatibles ; un changement de moteur ou de modèle ne
  réutilise pas leurs transcriptions. Empêcher les écritures concurrentes dans
  un même traitement par un verrou libérable après crash.
- Conserver audio et résultats acquis après échec ou interruption. Après
  réussite de l'export, conserver la transcription réutilisable et supprimer
  l'audio du traitement. `cache clean` purge les traitements inactifs, sans
  toucher aux exports, modèles installés, configuration ni trousseau.
- Autoriser au maximum trois nouvelles tentatives après l'essai initial pour
  les erreurs temporaires, avec délai croissant et possibilité d'annulation.
  Arrêter immédiatement sur erreur permanente : clé invalide, format refusé,
  dépendance absente ou entrée inaccessible.
- Réutiliser les réponses sauvegardées pour éviter des appels inutiles. Une
  requête distante dont l'issue est inconnue après une coupure peut avoir été
  facturée : la reprise locale ne constitue pas une garantie d'exécution distante unique.
- Sur interruption, arrêter les requêtes et processus enfants, fermer les
  fichiers et laisser un manifeste récupérable. Garder les fichiers temporaires
  distincts des exports finaux et éviter de journaliser les transcriptions.

## Contrat des exports

| Format | Contenu |
| --- | --- |
| TXT | Texte seul en UTF-8, ponctuation et paragraphes, sans métadonnées ajoutées |
| Markdown | Titre, URL source, métadonnées utiles et transcription lisible |
| JSON | Objet versionné avec provenance, paramètres effectifs, texte et segments |

Le JSON inclut `schema_version`, la vidéo (`id`, `url`, `title`, `channel`,
`duration_seconds`), `backend`, `model`, `language`, `text` et `segments`.
Chaque segment porte `start`, `end` en secondes depuis le début de la vidéo et
`text`. Les timestamps doivent être cohérents, ordonnés et dans la durée audio.
Une métadonnée indisponible est explicitement nulle ; une liste de segments vide
ne doit pas suggérer que des timestamps ont été fournis. Établir le schéma exact
et ses fixtures lors de l'implémentation, puis gérer explicitement son évolution.

## Critères d'acceptation

- Une vidéo accessible sans connexion produit un TXT par défaut ; les variantes
  Markdown et JSON conservent le même contenu transcrit et la bonne provenance.
- Les deux moteurs satisfont le même contrat sans dépendance au moteur non choisi.
- Sans moteur configuré, la commande fournit une action claire, sans appel payant.
- Les durées de quatre heures sont admises ; les dépassements et directs en
  cours sont rejetés avant transcription.
- Une interruption au milieu d'un traitement reprend les segments manquants,
  avec horodatages globaux corrects et sans réexécuter les segments sauvegardés.
- Une transcription complète en cache peut être réexportée sans réseau, clé
  API, modèle local ni outils audio.
- Une clé invalide ne déclenche aucune nouvelle tentative ; un échec temporaire
  respecte la borne de tentatives et préserve le cache.
- Annuler la commande arrête ses enfants et laisse un état reprenable.
- Un fichier existant est préservé sans `--force` ; un export échoué ne laisse
  aucun résultat partiel présenté comme final.
- Les tests couvrent chemins avec espaces, titres inhabituels, ordre des segments,
  séparation stdout/stderr et absence de secrets dans les données persistées.
- Un audio silencieux ne donne lieu à aucun texte inventé par l'application.
- Les releases sont compilées pour les quatre cibles et font l'objet d'essais
  représentatifs macOS/Linux ; tout essai non effectué est indiqué comme tel.

## Sources techniques à consulter selon l'intégration

- Mistral : [API de transcription](https://docs.mistral.ai/api/endpoint/audio/transcriptions),
  [guide audio](https://docs.mistral.ai/studio/audio/speech-to-text/transcribe-files),
  [limites](https://docs.mistral.ai/studio/audio/specs-and-limits/limits).
- YouTube : [options et exécutables yt-dlp](https://github.com/yt-dlp/yt-dlp),
  [runtime JavaScript et EJS](https://github.com/yt-dlp/yt-dlp/wiki/EJS).
- Local : [whisper.cpp, modèles et accélération](https://github.com/ggml-org/whisper.cpp).
- Audio : [documentation FFmpeg](https://ffmpeg.org/documentation.html).
- Builds : [cibles Go](https://go.dev/doc/install/source#environment).
