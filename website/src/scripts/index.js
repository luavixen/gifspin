import { observe, computed, ignore } from 'patella';

import { apiUpload, apiSpin } from './api.js';

const envMaxFileSize = 5 * 1024 * 1024;
const envResolutionTarget = 400;

const speedConfigs = [
    { frameCount: 90, frameDelay: 32, name: 'Slowest' },
    { frameCount: 60, frameDelay: 32, name: 'Slow'    },
    { frameCount: 60, frameDelay: 16, name: 'Normal'  },
    { frameCount: 48, frameDelay: 16, name: 'Fast'    },
    { frameCount: 32, frameDelay: 16, name: 'Faster'  },
    { frameCount: 24, frameDelay: 16, name: 'Fastest' },
    { frameCount: 16, frameDelay: 16, name: 'HELP'    }
];

for (const speedConfig of speedConfigs) ignore(speedConfig);

const $ = (id) => document.getElementById(id);

const $settings               = $('gs-settings')
    , $settingsFile           = $('gs-settings-file')
    , $settingsSpeed          = $('gs-settings-speed')
    , $settingsDirectionRight = $('gs-settings-direction-right')
    , $settingsDirectionLeft  = $('gs-settings-direction-left')
    , $settingsOptionsCrop    = $('gs-settings-options-crop')
    , $settingsOptionsFlatten = $('gs-settings-options-flatten')
    , $settingsColor          = $('gs-settings-color')
    , $settingsColorPicker    = $('gs-settings-color-picker')
    , $settingsColorText      = $('gs-settings-color-text')
    , $settingsSubmitApply    = $('gs-settings-submit-apply')
    , $settingsSubmitDownload = $('gs-settings-submit-download')
    , $previewOverlay         = $('gs-preview-overlay')
    , $previewIconPlaceholder = $('gs-preview-icon-placeholder')
    , $previewIconLoading     = $('gs-preview-icon-loading')
    , $previewError           = $('gs-preview-error')
    , $previewErrorContent    = $('gs-preview-error-content')
    , $previewImage           = $('gs-preview-image');

$settings.addEventListener('submit', (ev) => ev.preventDefault());

$settingsFile.value = '';
$settingsSpeed.value = '2';
$settingsDirectionRight.checked = true;
$settingsDirectionLeft.checked = false;
$settingsOptionsCrop.checked = false;
$settingsOptionsFlatten.checked = false;

const settings = observe({
    speedIndex: 2,
    speedConfig: null,

    flagReverse: false,

    flagCrop: false,
    flagFlatten: false,

    colorString: '#0eaaaa',
    colorValue: 0
});

computed(() => {
    settings.speedConfig = speedConfigs[settings.speedIndex] ?? null;
});

$settingsSpeed.addEventListener('change', () => {
    settings.speedIndex = Number($settingsSpeed.value);
});

$settingsDirectionRight.addEventListener('change', () => {
    if ($settingsDirectionRight.checked) settings.flagReverse = false;
});

$settingsDirectionLeft.addEventListener('change', () => {
    if ($settingsDirectionLeft.checked) settings.flagReverse = true;
});

$settingsOptionsCrop.addEventListener('change', () => {
    settings.flagCrop = !!$settingsOptionsCrop.checked;
});

$settingsOptionsFlatten.addEventListener('change', () => {
    settings.flagFlatten = !!$settingsOptionsFlatten.checked;
});

const colorParse = (string) => {
    if (string == null) return null;

    let result = /([\da-f]{6})/i.exec(string);
    if (result == null) return null;

    return parseInt(result[1], 16);
};

const colorStringify = (value) => {
    return '#' + Number(value).toString(16);
};

computed(() => {
    settings.colorValue = colorParse(settings.colorString);
});

computed(() => {
    $settingsColor.disabled = !settings.flagFlatten;
});

computed(() => {
    $settingsColorPicker.value = settings.colorString;
});

$settingsColorPicker.addEventListener('change', () => {
    settings.colorString = $settingsColorText.value = $settingsColorPicker.value;
});

$settingsColorText.value = '';
$settingsColorText.placeholder = settings.colorString;

$settingsColorText.addEventListener('input', () => {
    let value = colorParse($settingsColorText.value);
    if (value != null) settings.colorString = colorStringify(value);
});

$settingsColorText.addEventListener('change', () => {
    let value = colorParse($settingsColorText.value);
    if (value != null) $settingsColorText.value = colorStringify(value);
});

const preview = observe({
    overlay: 'placeholder',
    content: null
});

const hide = ($el) => $el.classList.add('gs-hidden');
const show = ($el) => $el.classList.remove('gs-hidden');

computed(() => {
    hide($previewOverlay);
    hide($previewIconPlaceholder);
    hide($previewIconLoading);

    if (preview.overlay === 'placeholder') {
        show($previewIconPlaceholder);
        show($previewOverlay);
        return;
    }

    if (preview.overlay === 'loading') {
        show($previewIconLoading);
        show($previewOverlay);
        return;
    }
});

computed(() => {
    hide($previewError);
    hide($previewImage);

    let content = preview.content;
    if (content == null) return;

    if (content.type === 'error') {
        let error = content.error;
        if (error != null) $previewErrorContent.innerText = error;
        show($previewError);
        return;
    }

    if (content.type === 'image') {
        show($previewImage);
        return;
    }
});

const task = observe({
    value: null,
    locked: false
});

const taskNext = (provider) => {
    if (task.value == null) {
        task.value = (async () => {
            try {
                await provider();
            } catch (err) {
                console.error(err);
            } finally {
                task.value = null;
            }
        })();
    }
};

computed(() => {
    task.locked = task.value != null;
});

const state = observe({
    source: null,
    tokenInput: null,
    tokenOutput: null
});

computed(() => {
    $settingsFile.disabled = task.locked;
});

computed(() => {
    $settingsSubmitApply.disabled = task.locked || state.source == null;
});

computed(() => {
    $settingsSubmitDownload.disabled = task.locked || state.tokenOutput == null;
});

$settingsFile.addEventListener('change', () => {
    taskNext(async () => {

        let file = $settingsFile.files.item(0);
        if (file == null) return;

        preview.overlay = 'loading';
        preview.content = null;

        let urlPrev = state.source?.url;
        if (urlPrev != null) URL.revokeObjectURL(urlPrev);

        state.source = null;
        state.tokenInput = null;
        state.tokenOutput = null;

        if (file.size > envMaxFileSize) {
            preview.overlay = null;
            preview.content = ignore({ type: 'error', error: 'Uploaded image is too large, maximum file size is 5mb' });
            return;
        }

        let url = URL.createObjectURL(file);

        await new Promise((resolve, reject) => {

            $previewImage.decoding = 'sync';
            $previewImage.src = '';

            let onLoad = () => {
                let width = $previewImage.naturalWidth;
                let height = $previewImage.naturalHeight;

                if (width < 4 || height < 4) {
                    preview.overlay = null;
                    preview.content = ignore({ type: 'error', error: 'Uploaded image is invalid / too small' });
                } else {
                    preview.overlay = null;
                    preview.content = ignore({ type: 'image' });
                    state.source = ignore({ file, url, width, height });
                }

                removeListeners();
                resolve();
            };

            let onError = () => {
                preview.overlay = null;
                preview.content = ignore({ type: 'error', error: 'Uploaded image is invalid' });

                removeListeners();
                resolve();
            };

            let removeListeners = () => {
                $previewImage.removeEventListener('load', onLoad);
                $previewImage.removeEventListener('error', onError);
            };

            $previewImage.addEventListener('load', onLoad);
            $previewImage.addEventListener('error', onError);

            $previewImage.src = url;

        });

    });
});

const optionsGetResolution = () => {
    let actual = state.source;
    if (actual == null) return null;

    let target = { width: actual.width, height: actual.height };

    if (target.width > envResolutionTarget) {
        target = {
            width: envResolutionTarget,
            height: (target.height / target.width) * envResolutionTarget
        };
    }

    if (target.height > envResolutionTarget) {
        target = {
            width: (target.width / target.height) * envResolutionTarget,
            height: envResolutionTarget
        };
    }

    target = {
        width: Math.round(target.width),
        height: Math.round(target.height)
    };

    return ignore(target);
};

const optionsFromSettings = () => {
    let options = {};

    let resolution = optionsGetResolution();
    options.width = resolution.width;
    options.height = resolution.height;

    options.frameCount = settings.speedConfig.frameCount;
    options.frameDelay = settings.speedConfig.frameDelay;

    options.flagCrop = settings.flagCrop;
    options.flagReverse = settings.flagReverse;
    options.flagFlatten = settings.flagFlatten;

    if (settings.flagFlatten) {
        options.background = (settings.colorValue << 8 | 0xFF) >>> 0;
    } else {
        options.background = 0;
    }

    return ignore(options);
};

$settingsSubmitApply.addEventListener('click', () => {
    taskNext(async () => {

        let source = state.source;
        if (source == null) return;

        preview.overlay = 'loading';

        let tokenInput = state.tokenInput;
        if (tokenInput == null) {
            try {
                let { status, error, value } = await apiUpload(source.file);
                if (value != null) {
                    tokenInput = ignore(value);
                } else {
                    preview.overlay = null;
                    preview.content = ignore({
                        type: 'error',
                        error: 'Failed to upload source image, server responded with status ' + status + ', message:\n' + error
                    });
                    return;
                }
            } catch (err) {
                preview.overlay = null;
                preview.content = ignore({
                    type: 'error',
                    error: 'Failed to upload source image, unexpected internal error occurred:\n' + err
                });
                return;
            }
        }

        let tokenOutput;
        try {
            let { status, error, value } = await apiSpin(tokenInput, optionsFromSettings());
            if (value != null) {
                tokenOutput = ignore(value);
            } else {
                preview.overlay = null;
                preview.content = ignore({
                    type: 'error',
                    error: 'Failed to spin image, server responded with status ' + status + ', message:\n' + error
                });
                return;
            }
        } catch (err) {
            preview.overlay = null;
            preview.content = ignore({
                type: 'error',
                error: 'Failed to spin image, unexpected internal error occurred:\n' + err
            });
            return;
        }

        state.tokenInput = tokenInput;
        state.tokenOutput = tokenOutput;

        await new Promise((resolve, reject) => {

            let onLoad = () => {
                preview.overlay = null;
                preview.content = ignore({ type: 'image' });

                removeListeners();
                resolve();
            };

            let onError = () => {
                preview.overlay = null;
                preview.content = ignore({ type: 'error', error: 'Failed to load output image' });

                removeListeners();
                resolve();
            };

            let removeListeners = () => {
                $previewImage.removeEventListener('load', onLoad);
                $previewImage.removeEventListener('error', onError);
            };

            $previewImage.addEventListener('load', onLoad);
            $previewImage.addEventListener('error', onError);

            preview.content = null;

            $previewImage.decoding = 'sync';
            $previewImage.src = tokenOutput.file;

        });

    });
});

$settingsSubmitDownload.addEventListener('click', () => {
    taskNext(async () => {

        let source = state.source;
        if (source == null) return;

        let tokenOutput = state.tokenOutput;
        if (tokenOutput == null) return;

        let filename;
        filename = source.file.name;
        filename = filename.replace(/\.[^/.]*?$/, '');
        filename = filename + '-' + tokenOutput.file.substring(23, 27) + '.gif';

        let url = new URL(location.href);
        url.pathname = tokenOutput.file;
        url.search = String(new URLSearchParams([['attachment', filename]]));
        url.hash = '';

        let $link = document.body.appendChild(document.createElement('a'));
        $link.href = String(url);
        $link.click();
        $link.remove();

    });
});
