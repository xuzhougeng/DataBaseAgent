# Paste this file into the first DBA pipeline step.
archive="$DBA_INPUT/counts.tsv.gz"
input="$archive"
if [ ! -f "$archive" ]; then
  input="$archive.partial"
  curl --fail --location --continue-at - --max-time 120 --output "$input" "$DBA_SOURCE_URL"
fi
printf '%s  %s\n' '7dd92c7adc6111d132638660595fcff2c65205ec83a587d276f43b24c71c1855' "$input" | sha256sum -c -
if [ "$input" != "$archive" ]; then
  mv "$input" "$archive"
fi
