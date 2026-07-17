# Wiki source (tracked in-repo)

This folder is the **canonical** openfsd operator wiki. GitHub hosts a separate wiki git repository (`openfsd.wiki.git`); we keep the same Markdown here so docs review and version with application code.

## Syncing to the live GitHub wiki

One-time:

```bash
git clone https://github.com/renorris/openfsd.wiki.git /tmp/openfsd.wiki
```

Publish (from a clean checkout of this repo):

```bash
# Copy pages (not this README) into the wiki clone
rsync -a --delete \
  --exclude README.md \
  --exclude .git \
  wiki/ /tmp/openfsd.wiki/

cd /tmp/openfsd.wiki
git add -A
git status
git commit -m "Sync wiki from openfsd repo"
git push origin master
```

GitHub wiki default branch is usually `master`. Do not commit the nested `.git` of the wiki clone into the main repo.

## Page naming

GitHub wiki turns `Migrating-from-PostgreSQL.md` into the URL slug `Migrating-from-PostgreSQL`. Keep spaces as hyphens in filenames to match existing wiki links in the README.
