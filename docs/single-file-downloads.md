# Single File Download Examples for Ensembl Genomes

This guide shows how to download individual files or chromosomes from Ensembl databases using hapiq, instead of downloading entire databases with thousands of files.

## Quick Reference

### Format Patterns
```bash
# Species-specific downloads
DATABASE:VERSION:CONTENT:SPECIES_NAME

# Direct FTP URLs  
ftp://ftp.ensemblgenomes.org/pub/release-VERSION/DATABASE/...
```

### Available Commands
```bash
hapiq species [database] [version]    # Browse available species
hapiq check [identifier]              # Validate before downloading
hapiq download [identifier]           # Actually download files
```

## Method 1: Species-Specific Downloads

The easiest way to download single species data using the structured identifier format.

### Model Organisms
```bash
# Baker's yeast (S. cerevisiae) - all data types
hapiq check "fungi:47:gff3:saccharomyces_cerevisiae"    # Genome annotations
hapiq check "fungi:47:pep:saccharomyces_cerevisiae"     # Protein sequences
hapiq check "fungi:47:cds:saccharomyces_cerevisiae"     # Coding sequences
hapiq check "fungi:47:dna:saccharomyces_cerevisiae"     # Genomic DNA

# E. coli
hapiq check "bacteria:47:gff3:escherichia_coli_str_k_12_substr_mg1655"
hapiq check "bacteria:47:pep:escherichia_coli_str_k_12_substr_mg1655"

# Arabidopsis thaliana
hapiq check "plants:50:gff3:arabidopsis_thaliana"
hapiq check "plants:50:dna:arabidopsis_thaliana"

# Drosophila melanogaster
hapiq check "metazoa:47:gff3:drosophila_melanogaster"
hapiq check "metazoa:47:pep:drosophila_melanogaster"

# C. elegans
hapiq check "metazoa:47:gff3:caenorhabditis_elegans"
```

### Partial Matching (Convenient)
```bash
# These work with partial species names for convenience
hapiq check "fungi:47:pep:cerevisiae"           # Matches saccharomyces_cerevisiae
hapiq check "bacteria:47:gff3:escherichia"      # Matches E. coli strains
hapiq check "plants:50:dna:arabidopsis"         # Matches arabidopsis_thaliana
hapiq check "metazoa:47:cds:drosophila"         # Matches drosophila species
```

## Method 2: Browse Available Species

Use the built-in species browser to find exactly what you need:

```bash
# Browse fungi database
hapiq species fungi 47

# Browse bacteria with filtering
hapiq species bacteria 47 --filter "escherichia"

# Show detailed information
hapiq species plants 50 --all

# Show download examples
hapiq species fungi --examples

# JSON output for scripting
hapiq species bacteria --output json
```

## Method 3: Direct FTP URLs

For maximum precision, use direct FTP URLs to target specific files.

### Complete File Examples
```bash
# Single protein file (2.7 MB)
hapiq check "ftp://ftp.ensemblgenomes.org/pub/release-47/fungi/fasta/saccharomyces_cerevisiae/pep/Saccharomyces_cerevisiae.R64-1-1.pep.all.fa.gz"

# Single annotation file
hapiq check "ftp://ftp.ensemblgenomes.org/pub/release-47/fungi/gff3/saccharomyces_cerevisiae/Saccharomyces_cerevisiae.R64-1-1.47.gff3.gz"

# Complete genome assembly
hapiq check "ftp://ftp.ensemblgenomes.org/pub/release-47/fungi/fasta/saccharomyces_cerevisiae/dna/Saccharomyces_cerevisiae.R64-1-1.dna.toplevel.fa.gz"
```

### Individual Chromosomes
```bash
# Arabidopsis chromosome 1 (30 MB)
hapiq check "ftp://ftp.ensemblgenomes.org/pub/release-50/plants/fasta/arabidopsis_thaliana/dna/Arabidopsis_thaliana.TAIR10.dna.chromosome.1.fa.gz"

# Arabidopsis chromosome 2  
hapiq check "ftp://ftp.ensemblgenomes.org/pub/release-50/plants/fasta/arabidopsis_thaliana/dna/Arabidopsis_thaliana.TAIR10.dna.chromosome.2.fa.gz"

# Yeast chromosome I
hapiq check "ftp://ftp.ensemblgenomes.org/pub/release-47/fungi/fasta/saccharomyces_cerevisiae/dna/Saccharomyces_cerevisiae.R64-1-1.dna.chromosome.I.fa.gz"

# Rice chromosome 1
hapiq check "ftp://ftp.ensemblgenomes.org/pub/release-50/plants/fasta/oryza_sativa/dna/Oryza_sativa.IRGSP-1.0.dna.chromosome.1.fa.gz"
```

### Organellar Genomes
```bash
# Mitochondrial genomes
hapiq check "ftp://ftp.ensemblgenomes.org/pub/release-50/plants/fasta/arabidopsis_thaliana/dna/Arabidopsis_thaliana.TAIR10.dna.chromosome.Mt.fa.gz"

# Chloroplast genomes  
hapiq check "ftp://ftp.ensemblgenomes.org/pub/release-50/plants/fasta/arabidopsis_thaliana/dna/Arabidopsis_thaliana.TAIR10.dna.chromosome.Pt.fa.gz"
```

## Directory Structure Reference

### FTP Layout
```
ftp://ftp.ensemblgenomes.org/pub/release-VERSION/DATABASE/
├── fasta/SPECIES/
│   ├── pep/                              # Protein sequences
│   │   └── *.pep.all.fa.gz              # All proteins for species
│   ├── cds/                              # Coding sequences
│   │   └── *.cds.all.fa.gz              # All coding sequences
│   └── dna/                              # Genomic DNA
│       ├── *.dna.toplevel.fa.gz         # Complete genome
│       ├── *.dna.chromosome.*.fa.gz     # Individual chromosomes
│       └── *.dna.nonchromosomal.fa.gz   # Scaffolds/contigs
├── gff3/SPECIES/                         # Genome annotations
│   └── *.gff3.gz                        # Annotation files
└── species_EnsemblDATABASE.txt          # Species list
```

### Content Types
- **gff3**: Genome annotations (genes, exons, features)
- **pep**: Protein sequences (amino acids)
- **cds**: Coding DNA sequences (nucleotides)
- **dna**: Genomic DNA sequences

### Databases Available
- **bacteria**: Bacterial genomes (~50,000 species)
- **fungi**: Fungal genomes (~1,000 species)
- **metazoa**: Invertebrate animal genomes (~500 species)
- **plants**: Plant genomes (~100 species)
- **protists**: Protist genomes (~200 species)

## File Size Examples

| File Type | Example | Typical Size |
|-----------|---------|--------------|
| Single protein file | S. cerevisiae proteins | ~2.7 MB |
| Single GFF3 | S. cerevisiae annotations | ~3.2 MB |
| Single chromosome | Arabidopsis chr1 | ~30 MB |
| Complete genome | S. cerevisiae genome | ~12 MB |
| Large genome | Rice complete | ~400 MB |
| Individual chromosome | Human chr1 | ~250 MB |

## Workflow Examples

### Basic Validation and Download
```bash
# 1. Validate the identifier
hapiq check "fungi:47:pep:cerevisiae"

# 2. Download if valid
hapiq download "fungi:47:pep:cerevisiae" --output ./data/
```

### Browse and Select
```bash
# 1. Browse available species
hapiq species fungi 47 --filter "candida"

# 2. Pick specific species
hapiq check "fungi:47:gff3:candida_albicans"

# 3. Download
hapiq download "fungi:47:gff3:candida_albicans"
```

### Batch Downloads
```bash
# Create file with identifiers
cat > my_downloads.txt << EOF
fungi:47:pep:saccharomyces_cerevisiae
fungi:47:gff3:saccharomyces_cerevisiae
bacteria:47:pep:escherichia_coli
plants:50:dna:arabidopsis_thaliana
EOF

# Download all
hapiq download --input my_downloads.txt --output ./genomics_data/
```

### Chromosome Analysis Project
```bash
# Download all Arabidopsis chromosomes
for chr in {1..5} Mt Pt; do
  hapiq download "ftp://ftp.ensemblgenomes.org/pub/release-50/plants/fasta/arabidopsis_thaliana/dna/Arabidopsis_thaliana.TAIR10.dna.chromosome.${chr}.fa.gz" --output ./arabidopsis_chromosomes/
done
```

## Performance and Concurrency

### FTP Connection Management
- **Connection pooling**: Reuses FTP connections efficiently
- **Rate limiting**: 1 request per second to respect server limits
- **Concurrent downloads**: Maximum 2 concurrent FTP connections
- **Automatic retry**: Failed connections are retried automatically

### Configuration Options
```bash
# Adjust concurrency (use carefully)
hapiq download "fungi:47:pep:cerevisiae" --max-concurrent 1

# Increase timeout for large files
hapiq download "plants:50:dna:triticum" --timeout 300s

# Skip existing files
hapiq download "bacteria:47:pep:all" --skip-existing
```

## Common Use Cases

### Comparative Genomics
```bash
# Download same data type for multiple related species
hapiq download "fungi:47:pep:saccharomyces_cerevisiae"
hapiq download "fungi:47:pep:candida_albicans"
hapiq download "fungi:47:pep:aspergillus_fumigatus"
```

### Genome Assembly Analysis
```bash
# Download specific chromosomes for detailed analysis
hapiq download "ftp://ftp.ensemblgenomes.org/pub/release-50/plants/fasta/arabidopsis_thaliana/dna/Arabidopsis_thaliana.TAIR10.dna.chromosome.1.fa.gz"
hapiq download "ftp://ftp.ensemblgenomes.org/pub/release-50/plants/fasta/arabidopsis_thaliana/dna/Arabidopsis_thaliana.TAIR10.dna.chromosome.2.fa.gz"
```

### Annotation-Only Analysis
```bash
# Just the annotations without sequence data
hapiq download "plants:50:gff3:arabidopsis_thaliana"
hapiq download "metazoa:47:gff3:drosophila_melanogaster"
```

### Protein Study
```bash
# Protein sequences from model organisms
hapiq download "fungi:47:pep:saccharomyces_cerevisiae"
hapiq download "bacteria:47:pep:escherichia_coli"
hapiq download "metazoa:47:pep:caenorhabditis_elegans"
```

## Troubleshooting

### Common Issues
1. **Species name not found**: Use `hapiq species [database]` to find exact names
2. **FTP connection timeout**: Use `--timeout` flag to increase timeout
3. **File not found**: Check version numbers and file paths
4. **Large download interrupted**: Use `--skip-existing` to resume

### Debugging
```bash
# Verbose output
hapiq download "fungi:47:pep:cerevisiae" --verbose

# Check what would be downloaded
hapiq check "fungi:47:pep:cerevisiae" --output json

# Test FTP connectivity
hapiq check "ftp://ftp.ensemblgenomes.org/pub/release-47/fungi/species_EnsemblFungi.txt"
```

## Best Practices

1. **Always validate first**: Use `hapiq check` before downloading
2. **Use species-specific identifiers**: Avoid downloading entire databases
3. **Check file sizes**: Large genomes can be hundreds of MB
4. **Use appropriate concurrency**: Don't overwhelm FTP servers
5. **Organize output directories**: Use meaningful directory structures
6. **Verify downloads**: Check file sizes and checksums after download

## Tips for Large-Scale Analysis

1. **Batch processing**: Use input files for multiple downloads
2. **Storage planning**: Check available disk space before large downloads
3. **Network considerations**: Large files may take time on slow connections
4. **Version consistency**: Use same release versions for comparative studies
5. **Backup important data**: Keep copies of critical datasets

This approach gives you **surgical precision** for genomics data downloads, allowing you to get exactly the data you need without unnecessary bulk downloads!