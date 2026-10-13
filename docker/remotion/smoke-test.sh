#!/bin/sh
set -eu

# Chạy trong image Remotion, dưới user goclaw; không sửa project có sẵn.
project_dir="${1:-/app/workspace/remotion-smoke}"
if [ -e "$project_dir" ]; then
  echo "Refusing to overwrite existing path: $project_dir" >&2
  exit 1
fi
cp -R /opt/remotion/template "$project_dir"
cp /opt/remotion/package.json /opt/remotion/package-lock.json "$project_dir/"
ln -s /opt/remotion/node_modules "$project_dir/node_modules"
cd "$project_dir"
mkdir -p public out
ffmpeg -v error -f lavfi -i sine=frequency=440:duration=2 public/tone.wav
node -e 'const fs = require("node:fs"); const p = "src/Root.tsx"; const s = fs.readFileSync(p, "utf8").replace("{AbsoluteFill,", "{Audio, staticFile, AbsoluteFill,").replace("<div style={{color:", "<Audio src={staticFile(\"tone.wav\")} /><div style={{color:"); fs.writeFileSync(p, s);'
remotion render src/index.tsx Hello out/hello.mp4 --concurrency=1
ffprobe -v error -show_entries stream=codec_name,width,height,r_frame_rate,nb_frames -show_entries format=duration,size -of json out/hello.mp4
