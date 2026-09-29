#!/bin/sh
# Regenerates the decoder fixtures. Needs sox and ffmpeg (with libmp3lame).
# Each FLAC has a WAV twin with identical samples so tests can compare them.
set -eu
cd "$(dirname "$0")"
sox -n -r 44100 -b 16 -c 2 tone-44k16.wav synth 0.5 sine 440 sine 660 gain -6
sox -n -r 96000 -b 24 -c 2 tone-96k24.wav synth 0.5 sine 1000 sine 1500 gain -6
sox -n -r 44100 -b 16 -c 1 mono-44k16.wav synth 0.5 sine 440 gain -6
for f in tone-44k16 tone-96k24 mono-44k16; do
  flac -s -f --best -o "$f.flac" "$f.wav"
done
ffmpeg -loglevel error -y -i tone-44k16.wav -c:a libmp3lame -b:a 320k tone-44k16.mp3
# No Xing/Info header: dr_mp3 can only learn the length by scanning, like a live transcode.
ffmpeg -loglevel error -y -i tone-44k16.wav -c:a libmp3lame -b:a 320k -write_xing 0 tone-44k16-noxing.mp3
# Split point is deliberately not on a FLAC block boundary.
sox tone-44k16.wav half-a.wav trim 0 10007s
sox tone-44k16.wav half-b.wav trim 10007s
flac -s -f -o half-a.flac half-a.wav
flac -s -f -o half-b.flac half-b.wav
rm half-a.wav half-b.wav
