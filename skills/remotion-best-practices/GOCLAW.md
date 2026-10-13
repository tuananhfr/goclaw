# Remotion in GoClaw Docker

This gateway contains Node.js, npm, Chromium, FFmpeg, Noto fonts, Remotion 4.0.535 and React 19.2.0.
The `remotion` command uses the preinstalled CLI. The official references in this skill cover authoring React compositions.

## Create a project without installing packages

Use a new directory **inside the current agent's configured workspace**. Commands below use a relative path from that workspace; replace `my-video` with a task-specific name. Inspect the directory first and never overwrite an existing project.

```sh
cp -R /opt/remotion/template my-video
cp /opt/remotion/package.json my-video/package.json
cp /opt/remotion/package-lock.json my-video/package-lock.json
ln -s /opt/remotion/node_modules my-video/node_modules
```

Edit `my-video/src/Root.tsx` and add components under `my-video/src/`. Put local media in `my-video/public/` and use `staticFile()`.
The image contains remotion, @remotion/cli, @remotion/bundler, @remotion/renderer, @remotion/shapes, @remotion/transitions, @remotion/captions, @remotion/google-fonts, @remotion/media, react and react-dom.
For additional packages use the gateway's authorized package-management flow. Do not disable its shell package-install policy.
For deployment elsewhere, replace the node_modules symlink by installing the committed lockfile on that machine.

## Check and render

```sh
cd my-video
remotion compositions src/index.tsx
remotion render src/index.tsx Hello out/hello.mp4 --concurrency=1
ffprobe -v error -select_streams v:0 -show_entries stream=codec_name,width,height,r_frame_rate -show_entries format=duration,size -of json out/hello.mp4
```

Only export when requested. Keep MP4s in the agent workspace so GoClaw can serve them through its normal file-storage flow. Return the real artifact path or file URL supported by the client; never invent a public URL.
The agent exec tool has a time limit. For a long render, launch it in the shell with all standard streams redirected, then poll its log and exit-code file in later exec calls:

```sh
(remotion render src/index.tsx Hello out/hello.mp4 --concurrency=1 >out/render.log 2>&1; echo $? >out/render.exit) </dev/null >/dev/null 2>&1 &
```

Use a fresh output directory for each render. Wait for `render.exit` to contain `0`, then validate the MP4 with ffprobe. An existing MP4 alone does not prove the render finished. On failure, inspect `render.log`; do not report success on timeout or a nonzero exit code.

## Docker-specific behavior

- Preserve `remotion.config.ts`: it sets the system Chromium executable, H.264, one render worker and disables writes to the image's shared node_modules cache.
- Alpine uses musl. Do not run `remotion browser ensure` here to download the glibc Chrome Headless Shell. Use `/usr/bin/chromium` through the config or `--browser-executable=/usr/bin/chromium`.
- Rendering shares the gateway's CPU and memory limit. Render one job at a time; do not increase concurrency without sizing the deployment.
- Studio is optional in this headless gateway: an in-container localhost URL is not automatically reachable from the user's browser. The compose overlay does not publish a Studio port. In this environment, replace the upstream open-Studio step with `remotion still src/index.tsx Hello out/preview.png --frame=30` and inspect the image using available image tools. Proceed with the requested MP4 export. Do not claim an interactive preview is open unless it has actually been opened and verified.
- Remotion licensing applies to commercial use; consult https://www.remotion.dev/license before production rollout.
