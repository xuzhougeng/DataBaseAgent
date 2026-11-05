# Curating an SK-N-SH perturbation RNA-seq dataset with DBA

This case applies the GPSAdb curation criteria to a fixed pair of public GEO series from the human neuroblastoma cell line SK-N-SH. It follows one accepted contrast from candidate review through a versioned SSH job to a local count archive. The run starts from submitter-provided gene counts and produces a standardized matrix, the sample assignments and per-sample count summaries. [GPSAdb](https://doi.org/10.1093/nar/gkac1066)

## Define the topic and find candidates

Create a project for *Homo sapiens* bulk RNA-seq. Include a single-gene loss-of-function contrast in SK-N-SH with a matched same-cell-line control and publicly available raw gene counts. Exclude comparisons between unperturbed sublines and single-cell assays. Run the fixed query in GEO with a fetch limit of 10.

```text
gse[Entry Type] AND (GSE299540[ACCN] OR GSE139235[ACCN])
```

The accession pair was chosen before the run to exercise one inclusion and one exclusion. Use [candidates.json](candidates.json) in **Topic & search → Import candidates JSON** if GEO search is unavailable. With either path, a second successful search or import should add zero candidates.

## Record decisions and sample groups

| GEO series | Decision and reason |
| --- | --- |
| [GSE299540](https://www.ncbi.nlm.nih.gov/geo/query/acc.cgi?acc=GSE299540) | Include the SK-N-SH siRNA comparison. The submitters knocked down FAM167A (DIORA1) and provided five targeting and five non-targeting samples with a raw count table. The series also contains separate CRISPRi experiments. |
| [GSE139235](https://www.ncbi.nlm.nih.gov/geo/query/acc.cgi?acc=GSE139235) | Exclude. RNA-seq compares the NC3 and S3 sublines derived from SK-N-SH without a targeted gene knockdown or knockout. |

For GSE299540, set the direct download URL to the SK-N-SH siRNA supplement. [Original GEO count table](https://ftp.ncbi.nlm.nih.gov/geo/series/GSE299nnn/GSE299540/suppl/GSE299540_SH_siRNA_raw_counts.txt.gz)

```text
https://ftp.ncbi.nlm.nih.gov/geo/series/GSE299nnn/GSE299540/suppl/GSE299540_SH_siRNA_raw_counts.txt.gz
```

Leave the sample groups blank for the first attempt to exercise the help request. The verified assignments are in [sample-groups.txt](sample-groups.txt). GSM9040574–GSM9040578 are non-targeting controls and GSM9040569–GSM9040573 are FAM167A knockdowns. The pipeline checks the supplied groups and matches the GEO sample identifiers to the `SH_siRNA_NTC_*` and `SH_siRNA_KD_*` columns. [GEO sample list](https://www.ncbi.nlm.nih.gov/geo/query/acc.cgi?acc=GSE299540)

## Lock and run the pipeline

Register a Linux SSH host with `curl`, `python3` and the standard [DBA execution tools](../../pipelines.md#servers). Give the pipeline one CPU per job and a concurrency of one. Paste the contents of each shell file into the command field of its step. Lock the pipeline and create jobs. The accepted series should produce one job.

| Step | Tool label | Command |
| --- | --- | --- |
| Fetch and verify public counts | `curl + sha256sum` | [fetch-counts.sh](fetch-counts.sh) |
| Validate groups and standardize the matrix | `Python 3` | [process-counts.sh](process-counts.sh) |

Declare three outputs, one path per line.

```text
counts.tsv
samples.tsv
sample_qc.tsv
```

Attempt 1 should stop after download with a message that the five controls and five knockdowns require confirmed assignments. In **Jobs**, open the help request, paste `sample-groups.txt` into the sample-group field, enter a note describing the GEO check and resume. Attempt 2 uses the same pipeline version and verifies the cached input before processing.

## Verify the archive

Open **Data & outputs** and export the project manifest. The three output files should be listed under the GSE299540 job. Open the job's `run_manifest.json` alongside the files to check the accession, final sample groups, review note, pipeline version, attempt number and per-file SHA-256 hashes. Compare the hashes with [the recorded run](run-evidence.json).

| Output | Contents | Observed bytes |
| --- | --- | ---: |
| `counts.tsv` | 58,735 gene rows by 10 samples, with GSM identifiers as column names | 2,586,831 |
| `samples.tsv` | Ten sample assignments and their corresponding source columns | 375 |
| `sample_qc.tsv` | Total counts and number of genes with nonzero counts for each sample | 298 |

## Manuscript text

We applied the DataBaseAgent workflow to two public GEO series from the human neuroblastoma cell line SK-N-SH. The GEO search returned both series. We retained the FAM167A siRNA contrast in GSE299540, which comprised five targeting and five non-targeting samples, and excluded GSE139235, which compared unperturbed sublines. DBA dispatched one job through OpenSSH to a loopback macOS host. We withheld the sample groups on the first attempt to test whether the workflow would request assistance. After we supplied the verified assignments, the job ran under the same locked pipeline version and reused the downloaded count table. DBA archived a matrix of 58,735 gene rows by 10 samples, the sample assignments and per-sample summaries, and recorded SHA-256 checksums in the run manifest. [GSE299540](https://www.ncbi.nlm.nih.gov/geo/query/acc.cgi?acc=GSE299540) · [GSE139235](https://www.ncbi.nlm.nih.gov/geo/query/acc.cgi?acc=GSE139235) · [Run evidence](run-evidence.json)

The run used the browser preview API to record the review and recovery decisions. Its scope was curation and archiving of submitted counts, with manual review and a single macOS SSH host. Repeat requests for the series-filtered GEO query returned HTTP 500 and appear in the evidence record. A separate preflight with the same accessions and no entry-type filter fetched ten records on each of two searches and added ten on the first and zero on the second.
