const fs = require('node:fs');
const path = require('node:path');
const html = fs.readFileSync(path.join(__dirname, 'index.html'), 'utf8');
const app = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
const css = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8');

const nav = html.match(/<nav class="nav">([\s\S]*?)<\/nav>/);
if (!nav) throw new Error('Top navigation was not found');
const pages = [...nav[1].matchAll(/data-page="([^"]+)"/g)].map(m => m[1]);
if (pages.length !== 2 || pages[0] !== 'workbench' || pages[1] !== 'album') {
  throw new Error(`Expected only Workbench and Album tabs, got: ${pages.join(', ')}`);
}
if (/data-page="gallery"|id="page-gallery"/.test(html)) {
  throw new Error('A standalone gallery page must not exist');
}
if (!/\.page\s*\{[^}]*display\s*:\s*none/i.test(css) || !/\.page\.active\s*\{[^}]*display\s*:\s*flex/i.test(css)) {
  throw new Error('Top-level pages must be hidden by default and displayed only when active');
}
if (/\.album-page\s*\{[^}]*display\s*:/i.test(css)) {
  throw new Error('Album page must not override the shared page visibility state');
}
if (!/id="image-library-dialog"/.test(html) || !/id="template-add-image"/.test(html)) {
  throw new Error('The gallery must be opened from the TX template component controls');
}
if (!/id="template-select"/.test(html) || !/id="template-save"/.test(html) || !/id="template-add"/.test(html)) {
  throw new Error('The TX template dropdown and preset management controls are incomplete');
}
if (/id="template-slots"/.test(html) || /renderTemplateSlots/.test(app)) {
  throw new Error('Template presets must be selected with a dropdown, not clickable preset cards');
}
for (const id of ['template-select','template-add','template-duplicate','template-delete','template-add-text','template-add-image','template-clear-background']) {
  if (!new RegExp('\\$\\(\\\'' + id + '\\\'\\)\\.(?:onchange|onclick)').test(app) && !app.includes(`$('${id}')`)) throw new Error(`Missing template interaction binding: ${id}`);
}
if (!/function normalizeTemplates/.test(app) || !/function createTemplatePreset/.test(app)) throw new Error('Template data migration or dynamic preset creation is missing');
const bindUIStart = app.indexOf('function bindUI()');
const initStart = app.indexOf('async function init()');
if (bindUIStart < 0 || initStart < 0 || bindUIStart > initStart || !/DOMContentLoaded',\(\)=>\{bindUI\(\);init\(\);\}/.test(app)) {
  throw new Error('All click handlers must be bound as soon as the DOM is ready, before asynchronous backend initialization');
}
const bindUIBody = app.slice(bindUIStart, app.indexOf('async function init()', bindUIStart));
if (!/renderTemplateSelect\(\);renderTemplateComponents\(\);renderTemplateProperties\(\)/.test(bindUIBody)) {
  throw new Error('Default preset options must be rendered immediately during UI binding');
}
const initBody = app.slice(initStart);
if (!(initBody.indexOf('await loadTemplates()') >= 0 && initBody.indexOf('await loadTemplates()') < initBody.indexOf("call('GetSettings')"))) {
  throw new Error('Template data must load before potentially slow device enumeration');
}
for (const id of ['template-add-text','template-add-image','gallery-import','template-clear-background','template-add','template-duplicate','template-delete','template-save']) {
  if (!bindUIBody.includes(`$('${id}')`)) throw new Error(`Template/gallery control is not bound during early UI initialization: ${id}`);
}
if (!/id="gallery-feedback"/.test(html)) throw new Error('Gallery modal must show action feedback inside its top layer');
const openGalleryBody = app.slice(app.indexOf('async function openImageLibrary()'), app.indexOf('async function loadGallery()', app.indexOf('async function openImageLibrary()')));
if (!(openGalleryBody.indexOf('dialog.showModal()') >= 0 && openGalleryBody.indexOf('dialog.showModal()') < openGalleryBody.indexOf('await loadGallery()'))) {
  throw new Error('Image library dialog must open immediately before loading gallery data');
}
const galleryRenderBody = app.slice(app.indexOf('async function renderImageLibrary()'), app.indexOf('async function importGallery()', app.indexOf('async function renderImageLibrary()')));
if (!/root\.appendChild\(card\)[\s\S]*galleryData\(asset\.id\)\.then/.test(galleryRenderBody)) {
  throw new Error('Image library action cards must render before asynchronous thumbnails finish loading');
}
if ((app.match(/async function importGallery\(/g)||[]).length !== 1) {
  throw new Error('Only one localized image import handler should exist');
}
if (!/if\(added&&\$\('image-library-dialog'\)\.open\)\$\('image-library-dialog'\)\.close\(\)/.test(app)) {
  throw new Error('Adding an image layer must close the gallery after success so the canvas result is visible');
}
if (!/if\(\$\('image-library-dialog'\)\.open\)\$\('image-library-dialog'\)\.close\(\)/.test(app)) {
  throw new Error('Setting a template background must close the gallery after success');
}
if (!/\.tx-side \.template-editor\s*\{[^}]*flex:\s*1 1 0[^}]*overflow-y:\s*auto/s.test(css)) {
  throw new Error('Component editor must scroll independently');
}
const txSettingsHeader = html.slice(html.indexOf('class="pane tx-pane"'), html.indexOf('class="template-strip"'));
const txSettings = txSettingsHeader.slice(txSettingsHeader.indexOf('class="tx-transmission"'), txSettingsHeader.indexOf('id="tx-toggle"'));
if (!txSettings || !txSettings.includes('id="quality-select"') || !txSettings.includes('id="repeat-check"') || !txSettings.includes('id="send-interval"')) {
  throw new Error('Image quality, repeat and interval controls must be grouped in the TX header before Start TX');
}
if (txSettings.indexOf('id="quality-select"') > txSettings.indexOf('id="repeat-check"') || txSettings.indexOf('id="repeat-check"') > txSettings.indexOf('id="send-interval"')) {
  throw new Error('TX settings must show quality, repeat switch and interval in the requested order');
}
if ((html.match(/id="quality-select"/g)||[]).length !== 1 || (html.match(/id="repeat-check"/g)||[]).length !== 1 || (html.match(/id="send-interval"/g)||[]).length !== 1) {
  throw new Error('TX setting controls must exist exactly once after being moved into the title bar');
}
if (html.includes('id="open-image-library"') || /class="tx-transmission"[\s\S]*?gallery\.open/.test(html)) {
  throw new Error('The transmission settings group must not contain an image gallery action');
}
if (html.indexOf('class="tx-transmission"') > html.indexOf('class="tx-side"')) {
  throw new Error('TX transmission settings must remain in the title bar, not the component editor sidebar');
}

function extractFunction(name) {
  const start = app.indexOf(`function ${name}(`);
  if (start < 0) throw new Error(`Missing function ${name}`);
  const bodyMarker = app.indexOf('){', start);
  const open = bodyMarker >= 0 ? bodyMarker + 1 : app.indexOf('{', start);
  let depth = 0;
  for (let i = open; i < app.length; i++) {
    if (app[i] === '{') depth++;
    else if (app[i] === '}' && --depth === 0) return app.slice(start, i + 1);
  }
  throw new Error(`Unclosed function ${name}`);
}
const templateHelpers = new Function('tr', `${extractFunction('defaultTemplatePresets')}
${extractFunction('normalizeTemplates')}
return {defaultTemplatePresets,normalizeTemplates};`)(
  (key, vars={}) => Object.entries(vars).reduce((value,[k,v])=>value.replaceAll(`{${k}}`,String(v)),key)
);
const initialPresets = templateHelpers.defaultTemplatePresets();
if (initialPresets.length !== 4 || initialPresets[0].name !== 'CQ' || initialPresets[3].name !== 'Blank') {
  throw new Error('Default templates must migrate to CQ, RST, 73 and Blank');
}
const migrated = templateHelpers.normalizeTemplates([
  {id:'legacy',name:'Legacy',objects:[
    {id:'orphan',type:'image',assetId:''},
    {id:'text',type:'text',text:'TEST',x:150,y:-10,width:120,height:110,fontSize:400,outlineWidth:30},
    {id:'last-rx',type:'last-rx-image',x:5,y:5,width:30,height:30}
  ]},
  {id:'legacy',name:'Duplicate ID',components:[]}
]);
if (migrated.length !== 2 || migrated[0].components.length !== 2 || migrated[0].components[0].x !== 150 || migrated[0].components[0].y !== -10 || migrated[0].components[0].width !== 100 || migrated[0].components[0].fontSize !== 160 || migrated[0].components[0].outlineWidth !== 20 || migrated[0].id === migrated[1].id) {
  throw new Error('Legacy template normalization must discard broken image layers, preserve off-canvas layer positions, bound legacy size values and deduplicate IDs');
}
if (!/id="tx-toggle"/.test(html) || /id="tx-start"|id="tx-stop"/.test(html)) {
  throw new Error('TX must use one combined start/stop control in its title bar');
}
const txHeader = html.slice(html.indexOf('class="pane tx-pane"'), html.indexOf('class="template-strip"'));
if (!(txHeader.indexOf('id="tx-toggle"') >= 0 && txHeader.indexOf('id="tx-toggle"') < txHeader.indexOf('id="tx-pill"'))) {
  throw new Error('The combined TX button must appear immediately before the TX IDLE/ACTIVE status');
}
if (!(html.indexOf('class="template-strip"') < html.indexOf('class="tx-main"'))) {
  throw new Error('Template selection must remain above the preview and component editor');
}
if (/id="tx-queue"|tx-queue-heading|TX 发送队列|加入 TX 队列/.test(html)) {
  throw new Error('The TX queue must not be visible in the workbench');
}
if (/gallery\.addQueue|加入 TX 队列|Add to TX Queue|TX キューに追加/.test(app)) {
  throw new Error('The image gallery must not expose a queue action');
}
if (/PREVIEW|QUALITY <b id="quality-label"/.test(html)) {
  throw new Error('The TX canvas must not have preview/quality text overlays');
}
const previewCanvasRules = [...css.matchAll(/\.tx-canvas canvas\s*\{[^}]*\}/gs)].map(m => m[0]);
const finalPreviewCanvasRule = previewCanvasRules.at(-1) || '';
if (!/width:\s*auto\s*!important/.test(finalPreviewCanvasRule) ||
    !/height:\s*auto\s*!important/.test(finalPreviewCanvasRule) ||
    !/max-width:\s*100%\s*!important/.test(finalPreviewCanvasRule) ||
    !/max-height:\s*100%\s*!important/.test(finalPreviewCanvasRule) ||
    !/object-fit:\s*contain\s*!important/.test(finalPreviewCanvasRule) ||
    /object-fit:\s*fill/.test(finalPreviewCanvasRule)) {
  throw new Error('The TX preview canvas must scale uniformly within the fixed viewport and preserve its intrinsic aspect ratio');
}
if (!/\.tx-canvas\s*\{[^}]*aspect-ratio:\s*4\s*\/\s*3/s.test(css)) {
  throw new Error('The TX preview viewport must retain its fixed 4:3 display aspect ratio');
}
const drawTemplateSource = extractFunction('drawTemplateToCanvas');
if (!/low:\[320,240\],medium:\[448,336\],high:\[640,480\]/.test(drawTemplateSource)) {
  throw new Error('All TX quality presets must retain the same 4:3 source aspect ratio');
}

if (!/function applyDecodedCallsign/.test(app) || !/applyDecodedCallsign\(u\.callsign\)/.test(app) || !/field\.dataset\.valueSource==='manual'/.test(app) || !/valueSource=\$\('tx-his-call'\)\.value\.trim\(\)\?'manual':''/.test(app)) {
  throw new Error('Decoded callsigns must populate HisCall while preserving manual edits');
}
if (!/StartTXRendered',image/.test(app)) {
  throw new Error('TX must submit only the current template image, without a UI queue');
}
if (/StartTXQueueRendered|StartTXQueue\(/.test(fs.readFileSync(path.join(__dirname, '..', 'app.go'), 'utf8'))) {
  throw new Error('Wails must not expose a multi-image TX queue API');
}
const ids = new Set([...html.matchAll(/\bid="([^"]+)"/g)].map(m => m[1]));
const refs = new Set([...app.matchAll(/\$\(["']([^"']+)["']\)/g)].map(m => m[1]));
const missing = [...refs].filter(id => !ids.has(id));
if (missing.length) throw new Error(`Missing DOM IDs: ${missing.join(', ')}`);
console.log('UI structure OK: Workbench + Album tabs, dropdown template presets, TX settings left of the single TX toggle, no gallery action in transmission settings, non-overlapping editor, fixed 4:3 preview, no TX queue, RX callsign auto-fill, all static DOM IDs bound.');

// Shared background, QSO/editor alignment, anti-flicker rendering and settings localization.
const txMainMarkup = html.slice(html.indexOf('<div class="tx-main">'), html.indexOf('<div class="spectrum-header"><b data-i18n="tx.spectrum">'));
const previewColumnMarkup = txMainMarkup.slice(txMainMarkup.indexOf('class="tx-preview-column"'), txMainMarkup.indexOf('<aside class="tx-side">'));
if (!previewColumnMarkup.includes('class="tx-qso-fields"') || previewColumnMarkup.indexOf('class="tx-qso-fields"') < previewColumnMarkup.indexOf('id="tx-progress"')) {
  throw new Error('QSO fields must be placed below the preview in the left TX column');
}
if (txMainMarkup.indexOf('class="tx-qso-fields"') > txMainMarkup.indexOf('<aside class="tx-side">')) {
  throw new Error('QSO fields must finish at the left-column edge, aligned with the template editor border');
}
if (!/class="tx-side"[\s\S]*?class="side-section template-editor"/.test(txMainMarkup) || !/\.tx-side \.template-editor\s*\{[^}]*flex:\s*1 1 auto[^}]*min-height:\s*0/s.test(css)) {
  throw new Error('The template component editor must stretch to the same bottom edge as the QSO panel');
}
const componentHeader = html.slice(html.indexOf('class="side-section template-editor"'), html.indexOf('id="template-components"'));
if (!(componentHeader.indexOf('id="template-clear-background"') >= 0 && componentHeader.indexOf('id="template-clear-background"') < componentHeader.indexOf('id="template-add-text"'))) {
  throw new Error('Clear shared background must be in the component title before Add Text');
}
if (/class="template-library-footer"|class="template-background-line"|id="template-background-name"/.test(html)) {
  throw new Error('The separate template background information panel must be removed');
}
if (!/map\(\(\{backgroundAssetId,\.\.\.template\}\)=>template\)/.test(extractFunction('saveTemplates')) || !/items\[i\]\.BackgroundAssetID = \"\"/.test(fs.readFileSync(path.join(__dirname, '..', 'app.go'), 'utf8'))) {
  throw new Error('Saving a template preset must strip legacy background references');
}
if (!/templateBackgroundAssetId/.test(app) || !/templateBackgroundAssetId/.test(fs.readFileSync(path.join(__dirname, '..', 'settings.go'), 'utf8')) || !/migrateLegacyTemplateBackground/.test(fs.readFileSync(path.join(__dirname, '..', 'app.go'), 'utf8'))) {
  throw new Error('Shared background must be persisted in application settings and migrate old per-template references');
}
if (!/const staging=document\.createElement\('canvas'\)/.test(extractFunction('renderTemplatePreview')) || !/ctx\.drawImage\(staging,0,0\)/.test(extractFunction('renderTemplatePreview'))) {
  throw new Error('Live template preview must render offscreen and atomically blit the completed frame');
}
if (!/imageElementCache/.test(app) || !/galleryImage\(backgroundID\)/.test(extractFunction('drawTemplateToCanvas'))) {
  throw new Error('Template bitmap assets must reuse decoded image caches during component edits');
}
if (/PTT Signal|signal-rts|signal-dtr/.test(html + app)) {
  throw new Error('RTS and DTR must be selected directly in the PTT mode dropdown without a second signal selector');
}
for (const key of ['settings.configuration','audio.rxDevice','audio.txDevice','audio.default','audio.sampleRate','audio.duplexDetail','station.title','station.callsign','station.locator']) {
  if (!html.includes(`data-i18n="${key}"`)) throw new Error(`Missing localized settings label: ${key}`);
}
for (const key of ['audio.deviceCount','ptt.noPorts','ptt.portCount','settings.saved']) {
  if (!app.includes(`'${key}'`) && !fs.readFileSync(path.join(__dirname, '..', 'internal', 'i18n', 'locales.go'), 'utf8').includes(`"${key}"`)) {
    throw new Error(`Missing localized runtime settings message: ${key}`);
  }
}
console.log('TX settings regression OK: stable offscreen preview, shared non-template background, localized settings, dropdown-only RTS/DTR mode, QSO/editor bottom alignment.');

// Locale changes must repaint generated settings options as well as static labels.
if (!/renderPorts\(true\);if\(state\.audioDevicesLoaded\)renderAudioDeviceOptions\(true\)/.test(extractFunction('applyLocale'))) {
  throw new Error('Changing languages must re-render dynamically generated serial and audio device options');
}
if (!/tr\('audio\.defaultDevice'\)/.test(extractFunction('renderAudioDeviceOptions')) || !/preserveCurrent&&sel\.dataset\.optionsRendered/.test(extractFunction('renderAudioDeviceOptions'))) {
  throw new Error('The default-device suffix must use the active language rather than a hard-coded English label');
}
if (!/tr\('audio\.ready'\)/.test(extractFunction('renderAudioState')) || !/tr\('audio\.off'\)/.test(extractFunction('renderAudioState'))) {
  throw new Error('Audio readiness labels must follow the selected language');
}
const settingsModal = html.slice(html.indexOf('id="settings-modal"'), html.indexOf('</div>\n  </div>', html.indexOf('id="settings-modal"')));
if (/\bDefault\b|\bRefresh audio devices\b|\bStation identity\b|\bApplication Settings\b/.test(settingsModal)) {
  throw new Error('The settings modal must not hard-code English labels in the Simplified Chinese view');
}
if (!/\.tx-main\s*\{[^}]*column-gap:\s*0[^}]*row-gap:\s*0/s.test(css)) {
  throw new Error('QSO right edge must align with the left border of the template component editor');
}
console.log('Localization and alignment regression OK: dynamic device labels repaint on locale change, and QSO/editor column boundaries align.');

const previewRenderer = extractFunction('renderTemplatePreview');
if (!(previewRenderer.indexOf('if(version!==state.previewRenderVersion)') < previewRenderer.indexOf('ctx.clearRect(0,0,visibleCanvas.width,visibleCanvas.height)'))) {
  throw new Error('The visible canvas must not be cleared until the newest offscreen render is ready');
}
if (/baseAsset|drawTemplateToCanvas\([^)]*baseAsset/.test(app)) {
  throw new Error('Individual template assets must not be able to override the shared workbench background');
}
console.log('Preview and background regression OK: stale renders never clear the visible canvas, and template presets cannot override the shared background.');


// Audio waterfall, display preferences, component-specific property controls and contour outlines.
for (const id of ['rx-spectrum','tx-spectrum']) {
  const match = html.match(new RegExp(`<canvas id="${id}"[^>]*>`));
  if (!match || !html.slice(Math.max(0,html.indexOf(match[0])-120),html.indexOf(match[0])+match[0].length+180).includes('waterfall')) {
    throw new Error(`${id} must be rendered inside an audio waterfall panel`);
  }
}
if (!/id="settings-page-display"/.test(html) || !/data-settings-tab="display"/.test(html)) {
  throw new Error('The settings dialog must include a dedicated Display Settings page');
}
for (const id of ['waterfall-min-frequency','waterfall-max-frequency','waterfall-min-db','waterfall-max-db','waterfall-rows','waterfall-history']) {
  if (!new RegExp(`id="${id}" type="range"`).test(html)) throw new Error(`Waterfall parameter ${id} must be a slider`);
}
for (const id of ['waterfall-background-color','waterfall-low-color','waterfall-mid-color','waterfall-high-color','waterfall-peak-color']) {
  if (!new RegExp(`id="${id}" type="color"`).test(html)) throw new Error(`Waterfall color ${id} must be customizable`);
}
const maxFrequencySlider = html.match(/<input id="waterfall-max-frequency"[^>]*>/)?.[0] || '';
const normalizedWaterfallSource = extractFunction('normalizeWaterfallSettings');
if (!/max="48000"/.test(maxFrequencySlider) || !/maxFrequencyHz,5000,100,48000/.test(normalizedWaterfallSource)) {
  throw new Error(`The waterfall frequency setting must stay within 48 kHz Nyquist. slider=${maxFrequencySlider}; normalize=${normalizedWaterfallSource}`);
}
const waterfallGrid = extractFunction('renderWaterfallGrid');
if (!/scaleY=h\/view\.buffer\.height/.test(waterfallGrid) || !/destinationHeight=rows\*scaleY/.test(waterfallGrid)) {
  throw new Error('Waterfall history rows must be mapped proportionally onto the visible canvas height');
}
const waterfallDraw = extractFunction('drawWaterfallData');
if (!/view\.frequencyStart=minHz;view\.frequencyEnd=maxHz/.test(waterfallDraw) || !/putImageData\(data,0,view\.cursor\)/.test(waterfallDraw)) {
  throw new Error('Waterfall rows must be appended to the scrolling history buffer and use the actual available frequency range');
}
const componentControls = ['component-image-scale','component-font-size','component-outline-width'];
if (/id="component-(?:x|y|width|height)(?:-value)?"/.test(html) || /\$\('component-(?:x|y|width|height)'\)/.test(app)) {
  throw new Error('Position controls and independent image width/height controls must be removed; placement is by canvas dragging and image scaling is proportional');
}
for (const id of componentControls) if (!new RegExp(`id="${id}" type="range"`).test(html)) throw new Error(`${id} must be a slider`);
const propertyRenderer = extractFunction('renderTemplateProperties');
if (!/component-image-scale-field'\)\.hidden=!isImageComponent/.test(propertyRenderer) || !/component-font-size-field'\)\.hidden=!isText/.test(propertyRenderer) || !/component-color-field'\)\.hidden=!isText/.test(propertyRenderer) || !/component-source-field'\)\.hidden=!isImage/.test(propertyRenderer) || !/component-image-scale'\)\.disabled=!isImageComponent/.test(propertyRenderer)) {
  throw new Error('Text, gallery image and Last RX components must expose their own applicable controls');
}
if (!/state\.gallery\.length\?tr\('template\.unselected'\):tr\('template\.noImage'\)/.test(extractFunction('fillAssetSelect'))) {
  throw new Error('An available image library must show the “Not selected” option, not an empty-library message');
}
if (!/function getImageAlphaBounds/.test(app) || !/getImageAlphaBounds\(img\)/.test(extractFunction('drawTemplateToCanvas')) || !/function drawAlphaContourOutline/.test(app)) {
  throw new Error('Image outline and selection bounds must follow the image alpha contour');
}
if (!/ctx\.strokeText\(line,layout\.x,ty\)/.test(extractFunction('drawTemplateToCanvas'))) {
  throw new Error('Text outline must follow glyph outlines using strokeText');
}
if (/ctx\.strokeRect\(/.test(extractFunction('drawTemplateToCanvas'))) {
  throw new Error('Do not draw oversized rectangular selection frames around glyph/image contours');
}
const textMeasure = extractFunction('measureTemplateText');
if (/component\.width|component\.height/.test(textMeasure) || /wrapTemplateText\(/.test(textMeasure) || /canvas\.width-edgePad-layout\.width|canvas\.height-edgePad-layout\.height/.test(textMeasure)) {
  throw new Error('Text layers must keep their full measured glyph bounds and may extend beyond the canvas edges');
}
const canvasDrag = extractFunction('bindTemplateCanvas');
if (!/c\.x=\(px-drag\.dx\)\/canvas\.width\*100/.test(canvasDrag) || !/c\.y=\(py-drag\.dy\)\/canvas\.height\*100/.test(canvasDrag) || /clamp\(/.test(canvasDrag)) {
  throw new Error('Canvas dragging must allow text and image layers to move freely outside the canvas');
}
if (!/fullyOutside/.test(canvasDrag) || !/bounds\.width\/2/.test(canvasDrag) || !/bounds\.height\/2/.test(canvasDrag)) {
  throw new Error('A fully off-canvas selected layer must remain recoverable by dragging from the empty canvas');
}
if (!/componentPosition\(c,canvas\)/.test(extractFunction('drawTemplateToCanvas'))) {
  throw new Error('Image and Last RX layers must use unclamped canvas-relative positions');
}
if (!/component-order|for\(const c of template\?\.components\|\|\[\]\)/.test(extractFunction('drawTemplateToCanvas'))) {
  throw new Error('Template components must remain composited in component-list order so upper layers can partially obscure lower layers');
}
if (!/drawLastRXPlaceholder\(ctx,c,0,0,canvas\)/.test(extractFunction('drawTemplateToCanvas')) || !/strokeRect/.test(extractFunction('drawLastRXPlaceholder')) || !/template\.rxPlaceholder/.test(extractFunction('drawLastRXPlaceholder'))) {
  throw new Error('Missing Last RX image must render a labeled placeholder with a visible frame');
}
if (/window\.(?:confirm|alert|prompt)\s*\(/.test(app) || /wails\.localhost/i.test(`${html}\n${app}\n${css}`)) {
  throw new Error('Native browser confirmation/alert/prompt dialogs and wails.localhost popup text must not be used');
}
console.log('Waterfall and component regression OK: rolling waterfalls, display preferences, proportional image scale, free layer positioning, alpha-contour outlines, modal-free actions, and Last RX placeholder.');

// Photoshop-like layer regressions: all object coordinates remain unconstrained
// by the 4:3 canvas, and the canvas itself clips the final composite.
const layoutHelpers = new Function('resolveTemplateText', 'tr', `${extractFunction('componentPosition')}\n${extractFunction('measureTemplateText')}\nreturn {componentPosition,measureTemplateText};`)(value => String(value ?? ''), key => key);
const measureTextForTest = layoutHelpers.measureTemplateText;
const mockTextContext = {
  font: '', save() {}, restore() {},
  measureText(value) {
    const fontSize = Number(this.font.match(/([0-9.]+)px/)?.[1] || 16);
    return { width: Array.from(String(value)).length * fontSize * 0.6 };
  }
};
for (const frame of [{width:320,height:240},{width:448,height:336},{width:640,height:480}]) {
  for (const position of [[0,0],[80,90],[99,99],[-15,120],[145,-30]]) {
    const component={fontSize:160,text:'LARGE TEXT WITH MULTIPLE WORDS 中文字符',bold:true,outline:true,outlineWidth:20,x:position[0],y:position[1]};
    const layout=measureTextForTest(mockTextContext,component,frame);
    const expectedX=frame.width*position[0]/100,expectedY=frame.height*position[1]/100;
    if(layout.lines.length!==1||layout.width<=0||layout.height<=0||Math.abs(layout.x-expectedX)>0.01||Math.abs(layout.y-expectedY)>0.01||layout.truncated) {
      throw new Error(`Text layer was constrained or truncated at ${frame.width}x${frame.height}, position=${position.join(',')}`);
    }
    if(position[0]===145&&layout.x+layout.width<=frame.width)throw new Error('Text positioned outside the canvas should retain its full off-canvas glyph bounds');
  }
}
const explicitLines=measureTextForTest(mockTextContext,{fontSize:48,text:'LINE ONE\nLINE TWO',x:5,y:5},{width:320,height:240});
if(explicitLines.lines.length!==2)throw new Error('Explicit line breaks must remain intact in text layers');
const componentPosition=layoutHelpers.componentPosition;
for(const position of [[99,99],[-20,120],[150,-35]]){
  const canvas={width:640,height:480},c={x:position[0],y:position[1]},pos=componentPosition(c,canvas);
  if(Math.abs(pos.x-canvas.width*position[0]/100)>0.01||Math.abs(pos.y-canvas.height*position[1]/100)>0.01)throw new Error('Component positioning must not clamp layers to the canvas frame');
}
const imageRectForScale=new Function(`${extractFunction('imageRectForScale')}; return imageRectForScale;`)();
for(const img of [{width:1600,height:900},{width:900,height:1600},{width:1000,height:1000}]){
  for(const scale of [5,35,50,100]){
    const rect=imageRectForScale(img,{width:640,height:480},scale),ratio=rect.width/rect.height;
    if(Math.abs(ratio-img.width/img.height)>0.00001||rect.width>640.01||rect.height>480.01)throw new Error(`Image aspect ratio changed at scale ${scale}`);
  }
}
const placeholderCtx={strokeCalls:0,labels:[],save(){},restore(){},fillRect(){},strokeRect(){this.strokeCalls++;},fillText(text){this.labels.push(String(text));}};
const drawPlaceholder=new Function('imageRectForScale','componentPosition','tr',`${extractFunction('drawLastRXPlaceholder')}\nreturn drawLastRXPlaceholder;`)(imageRectForScale,componentPosition,key=>key);
const placeholderBounds=drawPlaceholder(placeholderCtx,{x:90,y:90,scale:35,outlineWidth:0,outlineColor:'#ffffff'},0,0,{width:640,height:480});
if(placeholderCtx.strokeCalls!==1||!placeholderCtx.labels.includes('template.rxPlaceholder')||placeholderBounds.width<=0||placeholderBounds.height<=0||placeholderBounds.x<=640-placeholderBounds.width||placeholderBounds.y<=480-placeholderBounds.height)throw new Error('Missing Last RX placeholder must retain its visible frame and may extend beyond the canvas');
const dilateAlpha = new Function(`${extractFunction('dilateAlphaMask')}; return dilateAlphaMask;`)();
const dilated = dilateAlpha(new Uint8Array([255]),1,1,2);
const alphaAt = (dx,dy) => dilated.alpha[(dilated.padding+dy)*dilated.width+dilated.padding+dx];
if (alphaAt(0,0)!==255 || alphaAt(2,0)!==255 || alphaAt(1,1)!==255 || alphaAt(2,2)!==0 || alphaAt(3,0)!==0) {
  throw new Error('Image contour dilation must preserve a rounded alpha silhouette without square-corner expansion');
}
const imageDraw = extractFunction('drawTemplateToCanvas');
if (!/c\.type==='image'\|\|c\.type==='last-rx-image'/.test(imageDraw) || !/dilateAlphaMask\(sourceAlpha,dw,dh,radius\)/.test(extractFunction('drawAlphaContourOutline')) || !/imageRectForScale\(img,canvas,c\.scale\?\?35\)/.test(imageDraw) || !/else if\(c\.type==='last-rx-image'\)\{selectionRect=drawLastRXPlaceholder/.test(imageDraw)) {
  throw new Error('Image and Last RX components must share the bounded alpha-contour renderer and skip empty transparent outlines');
}
if (/for\(let r=1;r<=radius;r\+\+\)/.test(extractFunction('drawAlphaContourOutline')) || /mctx\.drawImage\(base/.test(extractFunction('drawAlphaContourOutline'))) {
  throw new Error('Image outlines must not repeatedly blit the full source bitmap for every contour sample');
}
console.log('Component regression OK: free off-canvas layer movement, visible canvas clipping, proportional image scaling, Last RX placeholder, and bounded alpha dilation.');
