# Curated end-to-end suite

Real downloads from real repositories. Each result is checked against facts
we know without hapiq: the checksums a repository publishes, which files a
filter should keep, exit codes. It is slow, needs the network and depends on
upstream records staying online, so CI doesn't run it (build tag `curated`).

## Running

```sh
go test -tags curated -timeout 0 -v ./test/curated                       # all cases
go test -tags curated -timeout 0 -v ./test/curated -run 'TestCurated/geo-'  # one provider
go test -tags curated -timeout 0 -v ./test/curated -run 'TestCurated/geo-001'
HAPIQ_BIN=./bin/hapiq go test -tags curated -timeout 0 -v ./test/curated   # a prebuilt binary
```

Without `HAPIQ_BIN`, the runner builds hapiq from this tree first, so a stale
installed binary is never the one tested. Cases run one at a time to be gentle
on the repositories; a full run downloads about 400 MB.

Each case gets:

- an empty directory, which is both `$OUT` and the working directory;
- a fresh `HOME` holding only `~/.hapiqrc` with the cache off, so every byte
  comes from the source and no local config leaks in;
- `hapiq` on `PATH`, pointing at the binary under test.

Other environment variables (`NCBI_API_KEY`, `HF_TOKEN`) pass through.

## Layout and IDs

One file per provider (`geo.yaml`, `zenodo.yaml`, …), and one per feature
for cases that span providers (`refetch.yaml`, `manifest.yaml`).

Every case is named `<file>-<NNN>-<description>`, e.g. `geo-001-include-ext`.
`<file>-<NNN>` is its permanent ID. Use it in BUGS.md, issues and commit
messages. Take the next free number in the file, and never renumber or reuse
an ID, even after a case is deleted. The runner checks names and refuses
duplicate IDs before it downloads anything.

## Writing a case

```yaml
- name: zenodo-001-filename-pattern
  why: glob selects 2 of 12 files; HClust_Pathway_* must not match "Pathway_*"
  steps:
    - run: hapiq download zenodo 14518762 --out $OUT -y --filename-pattern "Pathway_*"
      files:
        - {path: Pathway_genelist.rds, size: 20666, md5: f107354d6d075364a3c94e34f1ff5134}
        - {path: HClust_Pathway_celltype_specific_genelist.rds, absent: true}
      witness: {source: zenodo}
```

A case is a list of `steps`. Each step runs one `sh -c` line (any shell: `rm`,
`cp`, heredocs), then checks:

| key | meaning |
|---|---|
| `run` | shell to run; pass `-y` to hapiq, as there is no terminal |
| `exit` | expected exit code (default 0) |
| `fails` | `true`: any nonzero exit code |
| `output_contains`, `output_not_contains` | substrings of stdout and stderr combined |
| `files` | list of `{path, absent, size, min_size, md5, sha256}` |
| `only` | every file under `$OUT` must match one of these patterns; `[]` means empty |
| `witness` | fields of `hapiq.json` by dotted path, e.g. `source: geo`, `files.#: 2` (a length), `files.0.size: 123` |
| `skip_witness_invariant` | turn off the check below for this step |

**Paths** are matched with Go's `path.Match`. A pattern without `/` matches a
file's basename anywhere under `$OUT`, so you don't need to know hapiq's
folder layout. A pattern with `/` matches the path relative to `$OUT`, and
`*` does not cross `/`. A `files` entry must match exactly one file, or none
with `absent: true`.

**Witness invariant.** After every step, each file listed in each `hapiq.json`
under `$OUT` must exist with the recorded size and sha256. `refetch` relies on
this. Skip it only for steps that break it on purpose (deleting or changing a
file, an expected failure).

A step that fails stops the case. The last 60 lines of its output are printed.

## Rules for expected values

1. **Never take expected values from hapiq.** In order of preference, use:
   - the checksum the repository publishes (Zenodo and figshare md5, ENA
     `fastq_md5`, HF LFS sha256);
   - otherwise, `sha256sum` of a `curl` download.

   A curl-derived hash only proves the file was the same on two downloads,
   so prefer published checksums.
2. **Record the source at the top of the file:** which API or URL, and the
   date.
3. **Point at the bug.** A case that reproduces a bug gets a `why:` line with
   its BUGS.md date or issue number. A case that fails on purpose until a fix
   lands says so: `why: fails at 0.1.3; …`.
4. **Keep it small.** Use filters (`--filename-pattern`, `--include-ext`) so a
   case moves at most about 100 MB, unless large files are what you're
   testing.
5. **Pick stable records:** a versioned DOI, a tagged file, an accession that
   won't be withdrawn. If upstream changes a record, update the case and the
   date; don't loosen the check.
