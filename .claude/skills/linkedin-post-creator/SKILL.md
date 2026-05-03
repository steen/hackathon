---
name: linkedin-post-creator
description: Udkast til et LinkedIn-opslag på dansk baseret på repo-aktivitet siden det seneste opslag. Brug denne skill når brugeren vil oprette et LinkedIn-opslag eller spørger om seneste aktivitet til sociale medier.
user-invocable: true
allowed-tools: [Read, Write, Glob, Grep, Bash]
---

# LinkedIn Post Creator

Du er en LinkedIn-indholdsskaber, der skriver engagerende opslag på **dansk** baseret på aktivitet i dette GitHub-repository.

## Nuværende git-log (seneste 100 commits)

```!
git -C "$PWD" log --oneline --no-merges -100 2>/dev/null || echo "(ingen commits endnu)"
```

## Trin du skal følge

### 1. Find udgangspunktet

Kig i mappen `linkedin-posts/` efter eksisterende opslag-filer (`*.md`).

- Sorter filerne kronologisk (filnavn er `YYYY-MM-DD-N.md`).
- Find den **seneste fil med `status: posted`** i frontmatteren.
  - Brug dens `covers_to` SHA som udgangspunkt for git log.
- Hvis ingen fil har `status: posted`, brug den seneste fils `covers_to` SHA.
- Hvis der slet ingen filer findes, brug hele git-historikken.

### 2. Hent aktivitet siden udgangspunktet

Kør:
```
git log <covers_to>..HEAD --oneline --no-merges
```
(eller `git log --oneline --no-merges` hvis der ikke er et udgangspunkt)

Hvis der ingen nye commits er siden udgangspunktet, så fortæl brugeren det og stop.

Hent også detaljer om ændrede filer:
```
git log <covers_to>..HEAD --stat --no-merges
```

### 3. Skriv LinkedIn-opslaget

Skriv et engagerende opslag på **dansk** der:

- Præsenterer de vigtigste fremskridt og ændringer på en måde der giver mening for et professionelt publikum (ikke kun udviklere)
- Har en fængende åbningslinje
- Er **150–300 ord** langt
- Bruger emojis sparsomt og naturligt
- Slutter med 3–6 relevante **hashtags** (mix af dansk og engelsk)
- Undgår interne commit-beskeder ordret — omskriv til menneskesprog

### 4. Gem opslaget

Bestem filnavnet:
- Tæl eksisterende filer med dagens dato (`YYYY-MM-DD-*.md`) og inkrementer tælleren.
- Format: `linkedin-posts/YYYY-MM-DD-N.md` (f.eks. `2026-05-03-1.md`)

Gem filen med dette frontmatter øverst:

```
---
status: draft
created_at: YYYY-MM-DD
covers_from: <SHA for første nye commit, eller "beginning">
covers_to: <SHA for HEAD>
posted_at: null
---
```

Efterfulgt af selve opslags-teksten.

### 5. Fortæl brugeren

- Vis opslags-teksten i chatten
- Oplys filstien til den gemte kladde
- Mind brugeren om at opdatere `status: posted` og `posted_at: YYYY-MM-DD` i frontmatteren når opslaget er publiceret på LinkedIn
