import {Config} from '@remotion/cli/config';

// Alpine dùng Chromium của hệ thống, không tải Chrome Headless Shell bản glibc.
Config.setBrowserExecutable(process.env.REMOTION_BROWSER_EXECUTABLE || '/usr/bin/chromium');
Config.setConcurrency(1);
Config.setVideoImageFormat('jpeg');
Config.setCodec('h264');
// node_modules là symlink tới bộ thư viện chỉ đọc trong image.
Config.setCachingEnabled(false);
