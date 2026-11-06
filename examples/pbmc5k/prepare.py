#!/usr/bin/env python3
"""Join official Cell Ranger outputs by barcode; no analysis or subsampling."""
import argparse
import collections
import csv
import hashlib
import io
import json
import math
from pathlib import Path
import tarfile

URL = 'https://cf.10xgenomics.com/samples/cell-exp/3.0.2/5k_pbmc_v3/5k_pbmc_v3_analysis.tar.gz'
PAGE = 'https://www.10xgenomics.com/datasets/5-k-peripheral-blood-mononuclear-cells-pbm-cs-from-a-healthy-donor-v-3-chemistry-3-1-standard-3-0-2'
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('archive', type=Path)
parser.add_argument('--output', type=Path, default=Path(__file__).parent)
args = parser.parse_args()
args.output.mkdir(parents=True, exist_ok=True)
with tarfile.open(args.archive) as archive:
    def read(name):
        with archive.extractfile(name) as f:
            return list(csv.DictReader(io.StringIO(f.read().decode('utf-8'))))
    coords = read('analysis/tsne/2_components/projection.csv')
    groups = read('analysis/clustering/graphclust/clusters.csv')
clusters = {r['Barcode']: r['Cluster'] for r in groups}
barcodes = [r['Barcode'] for r in coords]
assert len(barcodes) == len(set(barcodes)) == len(groups) == len(clusters) == 5025
assert set(barcodes) == set(clusters)
counts = collections.Counter(clusters.values())
output = args.output / 'cells.tsv'
with output.open('w', newline='') as f:
    writer = csv.writer(f, delimiter='\t', lineterminator='\n')
    writer.writerow(['barcode', 'tsne_1', 'tsne_2', 'cluster'])
    for r in coords:
        assert all(math.isfinite(float(r[k])) for k in ['TSNE-1', 'TSNE-2'])
        writer.writerow([r['Barcode'], r['TSNE-1'], r['TSNE-2'], 'Cluster ' + clusters[r['Barcode']]])
metadata = dict(dataset='5k_pbmc_v3', provider='10x Genomics', license='CC BY 4.0', source_page=PAGE, source_archive=URL,
    source_sha256=hashlib.sha256(args.archive.read_bytes()).hexdigest(), cells=5025,
    embedding='Cell Ranger 3.0.2 t-SNE (2 components)', clustering='Cell Ranger graphclust',
    cluster_counts={f'Cluster {k}': counts[k] for k in sorted(counts, key=int)},
    cell_type_annotations=False, subsampling=False,
    cells_tsv_sha256=hashlib.sha256(output.read_bytes()).hexdigest())
(args.output / 'provenance.json').write_text(json.dumps(metadata, indent=2)+'\n')
print(json.dumps(metadata, indent=2))
