# Paste this file into the second DBA pipeline step after downloading counts.tsv.gz.
python3 - "$DBA_INPUT/counts.tsv.gz" <<'PY'
import csv
import gzip
import os
import sys

# GEO GSM identifiers and the corresponding columns in the submitted count table.
samples = [
    (f"GSM{start + i}", group, f"SH_siRNA_{suffix}_{i + 1}")
    for start, group, suffix in ((9040574, "control", "NTC"), (9040569, "knockdown", "KD"))
    for i in range(5)
]
groups = {}
for line in os.environ["DBA_SAMPLE_GROUPS"].splitlines():
    fields = [field.strip() for field in line.split(",")]
    if len(fields) != 2 or fields[0] in groups:
        sys.exit("Invalid or duplicate sample assignment. Check the GEO sample sheet.")
    sample, group = fields
    groups[sample] = group
if groups != {sample: group for sample, group, _ in samples}:
    sys.exit("Expected five confirmed controls and five confirmed knockdowns for GSE299540 SK-N-SH siRNA.")

totals = [0] * len(samples)
detected = [0] * len(samples)
gene_ids = set()
with gzip.open(sys.argv[1], "rt", encoding="utf-8", newline="") as source, \
        open("counts.tsv", "w", encoding="utf-8", newline="") as target:
    reader = csv.reader(source, delimiter="\t")
    header = next(reader)
    columns = ["gene_id", *(column for _, _, column in samples)]
    if any(header.count(column) != 1 for column in columns):
        sys.exit("Unexpected count-table columns.")
    gene_column, *sample_columns = [header.index(column) for column in columns]
    writer = csv.writer(target, delimiter="\t", lineterminator="\n")
    writer.writerow(["gene_id", *(sample for sample, _, _ in samples)])
    for row in reader:
        if len(row) != len(header):
            sys.exit(f"Unexpected column count on input line {reader.line_num}.")
        gene_id = row[gene_column]
        counts = [row[index] for index in sample_columns]
        if not gene_id or gene_id in gene_ids or not all(count.isascii() and count.isdecimal() for count in counts):
            sys.exit(f"Invalid gene ID or raw count on input line {reader.line_num}.")
        gene_ids.add(gene_id)
        values = [int(count) for count in counts]
        writer.writerow([gene_id, *values])
        for index, value in enumerate(values):
            totals[index] += value
            detected[index] += value > 0
if not gene_ids or not all(totals):
    sys.exit("The matrix has no genes or contains an empty sample library.")

with open("samples.tsv", "w", encoding="utf-8", newline="") as target:
    writer = csv.writer(target, delimiter="\t", lineterminator="\n")
    writer.writerow(["sample_id", "group", "source_column"])
    writer.writerows(samples)

with open("sample_qc.tsv", "w", encoding="utf-8", newline="") as target:
    writer = csv.writer(target, delimiter="\t", lineterminator="\n")
    writer.writerow(["sample_id", "total_counts", "detected_genes"])
    writer.writerows((sample, total, genes) for (sample, _, _), total, genes in zip(samples, totals, detected))

print(f"Validated {len(gene_ids)} genes and {len(samples)} samples.")
PY
