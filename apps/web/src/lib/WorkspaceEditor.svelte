<script lang="ts">
  import { tick } from 'svelte';

  type Template = { name: string; description: string };
  type SyntaxResult = { ok: boolean; message: string };
  let {
    source = $bindable(''),
    selectedTemplate = $bindable('v4'),
    signedIn,
    editorLoaded,
    busy = false,
    dirty = false,
    templates = [],
    syntaxResult = null,
    onload,
    onsave,
    onvalidate,
    ondeploy,
    onSignIn,
    oninput,
  }: {
    source?: string;
    selectedTemplate?: string;
    signedIn: boolean;
    editorLoaded: boolean;
    busy?: boolean;
    dirty?: boolean;
    templates?: Template[];
    syntaxResult?: SyntaxResult | null;
    onload: () => void;
    onsave: () => void;
    onvalidate: () => void;
    ondeploy: (template: string) => void;
    onSignIn?: () => void;
    oninput?: () => void;
  } = $props();

  let confirmReplace = $state(false);
  let scrollTop = $state(0);
  let cursor = $state({ line: 1, column: 1 });
  let tabIndents = $state(true);
  let lines = $derived(source.split('\n').length);
  let readOnly = $derived(!signedIn || !editorLoaded || busy);
  let templateList = $derived(templates.some((template) => template.name === 'v4')
    ? templates
    : [{ name: 'v4', description: 'Balanced combat, looting, and navigation.' }, ...templates]);
  let templateDescription = $derived(templateList.find((template) => template.name === selectedTemplate)?.description ?? '');
  const templateName = (name: string) => name === 'v4' ? 'Balanced' : name.charAt(0).toUpperCase() + name.slice(1);

  function updateCursor(element: HTMLTextAreaElement) {
    const preceding = element.value.slice(0, element.selectionStart).split('\n');
    cursor = { line: preceding.length, column: preceding[preceding.length - 1].length + 1 };
  }

  async function handleKeydown(event: KeyboardEvent) {
    const element = event.currentTarget as HTMLTextAreaElement;
    if (event.key === 'Escape') {
      tabIndents = false;
      return;
    }
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 's') {
      event.preventDefault();
      if (!readOnly && dirty) onsave();
      return;
    }
    if (event.key !== 'Tab' || !tabIndents || readOnly || event.ctrlKey || event.metaKey || event.altKey) return;
    event.preventDefault();
    const start = element.selectionStart;
    const end = element.selectionEnd;
    const lineStart = start === 0 ? 0 : source.lastIndexOf('\n', start - 1) + 1;
    let nextStart: number;
    let nextEnd: number;
    if (start === end && !event.shiftKey) {
      source = source.slice(0, start) + '  ' + source.slice(end);
      nextStart = nextEnd = start + 2;
    } else {
      // A selection ending at a new line does not include that next line.
      const blockEnd = end > start && source[end - 1] === '\n' ? end - 1 : end;
      const block = source.slice(lineStart, blockEnd).split('\n');
      const removed = block.map((line) => event.shiftKey ? (line.match(/^( {1,2}|\t)/)?.[0].length ?? 0) : 0);
      const replacement = block.map((line, index) => event.shiftKey ? line.slice(removed[index]) : '  ' + line).join('\n');
      source = source.slice(0, lineStart) + replacement + source.slice(blockEnd);
      nextStart = event.shiftKey ? Math.max(lineStart, start - removed[0]) : start + 2;
      nextEnd = event.shiftKey ? Math.max(nextStart, end - removed.reduce((sum, count) => sum + count, 0)) : end + block.length * 2;
    }
    oninput?.();
    await tick();
    element.setSelectionRange(nextStart, nextEnd);
    updateCursor(element);
  }

  function deploySelected() {
    confirmReplace = false;
    ondeploy(selectedTemplate);
  }
</script>

<section class="workspace-editor" aria-label="Robot code workspace" aria-busy={busy}>
  <header class="workspace-heading">
    <div>
      <p class="section-label">Robot workspace</p>
      <h2>Your strategy starts here.</h2>
      <p class="intro">Edit your Lua robot, check its syntax, then save a version for your next match.</p>
    </div>
    <a class="docs-link" href="#/docs/sdk">SDK reference <span aria-hidden="true">↗</span></a>
  </header>

  <div class="strategy-bar">
    <div class="strategy-select">
      <label for="workspace-strategy">Start from a strategy</label>
      <select id="workspace-strategy" bind:value={selectedTemplate} onchange={() => confirmReplace = false} disabled={busy || !signedIn}>
        {#each templateList as template}
          <option value={template.name}>{templateName(template.name)}</option>
        {/each}
      </select>
    </div>
    <p>{templateDescription}</p>
    <button class="button secondary" onclick={() => confirmReplace = true} disabled={busy || !signedIn}>Deploy strategy</button>
  </div>

  {#if confirmReplace}
    <div class="replace-confirmation" role="group" aria-label="Confirm strategy replacement">
      <div><strong>Replace main.lua with {templateName(selectedTemplate)}?</strong><p>This replaces your workspace file{dirty ? ' and discards your unsaved edits' : ''}. Save a version first to keep your current work.</p></div>
      <div class="actions"><button class="button secondary" onclick={() => confirmReplace = false}>Cancel</button><button class="button replace" onclick={deploySelected} disabled={busy}>Replace main.lua</button></div>
    </div>
  {/if}

  <div class="editor-frame">
    <div class="editor-toolbar">
      <div class="file-tab"><span class="file-icon" aria-hidden="true">λ</span><span>main.lua</span>{#if dirty}<span class="dirty-dot" aria-label="Unsaved changes"></span>{/if}</div>
      <span class="file-path">/workspace/main.lua</span>
      <button class="text-button" onclick={onload} disabled={busy || !signedIn}>{editorLoaded ? 'Reload file' : 'Load workspace'}</button>
    </div>
    {#if !signedIn}
      <div class="editor-empty">
        <span class="empty-icon" aria-hidden="true">{'</>'}</span>
        <h3>A workspace for your robot.</h3>
        <p>Sign in to load your persistent main.lua, edit its behavior, and save script versions.</p>
        {#if onSignIn}<button class="button primary" onclick={onSignIn}>Sign in to code</button>{:else}<a class="button primary" href="#/box">Open workspace setup</a>{/if}
      </div>
    {:else if !editorLoaded}
      <div class="editor-empty">
        <span class="empty-icon" aria-hidden="true">{'</>'}</span>
        <h3>Open your robot's code.</h3>
        <p>Load main.lua from your workspace to continue editing, or deploy a starter strategy above.</p>
        <button class="button primary" onclick={onload} disabled={busy}>{busy ? 'Working…' : 'Load workspace'}</button>
        <a class="setup-link" href="#/box">First time? Set up your workspace</a>
      </div>
    {:else}
      <div class="code-area">
        <div class="line-numbers" aria-hidden="true"><pre style:transform={`translateY(-${scrollTop}px)`}>{Array.from({ length: lines }, (_, index) => index + 1).join('\n')}</pre></div>
        <textarea
          aria-label="main.lua source code"
          aria-describedby="editor-keyboard-help"
          bind:value={source}
          readonly={readOnly}
          spellcheck="false"
          autocapitalize="off"
          autocomplete="off"
          wrap="off"
          placeholder="Your workspace file is empty. Write Lua here or deploy a strategy above."
          onkeydown={handleKeydown}
          onfocus={() => tabIndents = true}
          oninput={(event) => { updateCursor(event.currentTarget); oninput?.(); }}
          onclick={(event) => updateCursor(event.currentTarget)}
          onkeyup={(event) => updateCursor(event.currentTarget)}
          onselect={(event) => updateCursor(event.currentTarget)}
          onscroll={(event) => scrollTop = event.currentTarget.scrollTop}
        ></textarea>
      </div>
    {/if}
    <div class="editor-status">
      <span class:unsaved={dirty} role="status"><i aria-hidden="true"></i>{busy ? 'Workspace busy' : !signedIn ? 'Sign in to edit' : !editorLoaded ? 'No file loaded' : dirty ? 'Unsaved changes' : 'Saved to workspace'}</span>
      <div><span>Ln {cursor.line}, Col {cursor.column}</span><span>Lua 5.4</span><span>UTF-8</span></div>
    </div>
  </div>

  <div class="editor-actions">
    <p id="editor-keyboard-help"><kbd>Tab</kbd> indents · <kbd>Esc</kbd> then <kbd>Tab</kbd> leaves editor · <kbd>Ctrl/⌘ S</kbd> saves</p>
    <div class="actions"><button class="button secondary" onclick={onvalidate} disabled={readOnly || !source.trim()}>Check syntax</button><button class="button primary" onclick={onsave} disabled={readOnly || !dirty}>Save version</button></div>
  </div>

  {#if syntaxResult}
    <div class="syntax-result" class:syntax-error={!syntaxResult.ok} role={syntaxResult.ok ? 'status' : 'alert'}>
      <strong>{syntaxResult.ok ? 'Syntax check passed' : 'Syntax check failed'}</strong>
      <p>{syntaxResult.message}</p>
    </div>
  {/if}

  <footer class="workspace-footer"><p>Saving writes main.lua and stores a version. Register your robot to use the saved script in a match.</p><a href="#/box">Advanced: SSH &amp; workspace <span aria-hidden="true">↗</span></a></footer>
</section>

<style>
  .workspace-editor { color: #dce5ef; min-width: 0; }
  .workspace-heading { display: flex; justify-content: space-between; align-items: flex-start; gap: 24px; margin-bottom: 24px; }
  .section-label { color: #7bd4c5; font-size: 10px; letter-spacing: .15em; text-transform: uppercase; font-weight: 650; margin: 0 0 8px; }
  h2 { font-size: clamp(22px, 3vw, 30px); font-weight: 600; letter-spacing: -.035em; line-height: 1.2; margin: 0 0 8px; }
  .intro { color: #91a0b4; font-size: 13px; line-height: 1.6; margin: 0; max-width: 530px; }
  a { color: #a9c8cf; text-decoration: none; }
  a:hover { color: #7de4d1; }
  .docs-link { font-size: 12px; white-space: nowrap; padding-top: 5px; }
  .docs-link span { margin-left: 6px; }
  .strategy-bar { display: flex; gap: 20px; align-items: center; background: #121b29; border: 1px solid #273447; border-radius: 10px; padding: 16px; margin-bottom: 20px; }
  .strategy-select { flex: 0 0 180px; }
  label { display: block; color: #a7b6c8; font-size: 11px; margin-bottom: 7px; }
  select { color: #e0e8f1; background: #0d1522; border: 1px solid #34435a; border-radius: 5px; width: 100%; font: inherit; font-size: 12px; padding: 8px 10px; }
  .strategy-bar > p { color: #91a0b4; font-size: 12px; line-height: 1.65; flex: 1; margin: 0; }
  .button { display: inline-flex; align-items: center; justify-content: center; min-height: 36px; border: 1px solid transparent; border-radius: 6px; padding: 8px 13px; font: inherit; font-size: 12px; font-weight: 600; cursor: pointer; white-space: nowrap; text-decoration: none; }
  .primary { background: #73d7c4; color: #0b2424; border-color: #73d7c4; }
  .primary:hover { background: #8ce6d5; color: #0b2424; }
  .secondary { background: #1a2636; border-color: #35445a; color: #d3dfed; }
  .secondary:hover { background: #243348; }
  button:disabled, select:disabled { opacity: .45; cursor: not-allowed; }
  .editor-frame { border: 1px solid #2b394d; border-radius: 9px; overflow: hidden; background: #0a121f; }
  .editor-toolbar { display: flex; min-height: 45px; align-items: center; gap: 16px; background: #141e2d; border-bottom: 1px solid #2a374a; }
  .file-tab { align-self: stretch; display: flex; align-items: center; gap: 10px; border-right: 1px solid #2a374a; border-top: 2px solid #73d7c4; background: #0a121f; padding: 0 18px; min-width: 144px; font-size: 12px; font-family: var(--font-mono, monospace); }
  .file-icon { color: #80bfff; font-size: 17px; }
  .dirty-dot { width: 6px; height: 6px; border-radius: 50%; background: #e4be77; }
  .file-path { color: #6f8097; font-family: var(--font-mono, monospace); font-size: 11px; }
  .text-button { margin-left: auto; margin-right: 14px; padding: 6px 0; background: none; border: 0; color: #a8bccf; cursor: pointer; font: inherit; font-size: 11px; }
  .text-button:hover { color: #7de4d1; }
  .code-area { display: flex; height: 440px; min-height: 280px; }
  .line-numbers { flex-shrink: 0; width: 54px; overflow: hidden; text-align: right; background: #0c1522; user-select: none; }
  .line-numbers pre { color: #566a83; padding: 20px 14px 20px 0; margin: 0; }
  .line-numbers pre, textarea { font-family: var(--font-mono, 'SFMono-Regular', Consolas, monospace); font-size: 12px; line-height: 22px; font-variant-ligatures: none; tab-size: 2; }
  textarea { display: block; flex: 1; min-width: 0; width: 100%; height: 100%; box-sizing: border-box; padding: 20px 18px; margin: 0; background: #0a121f; border: none; border-radius: 0; resize: none; color: #cbd9e9; outline-offset: -3px; caret-color: #83e0ce; }
  textarea::placeholder { color: #667b93; }
  textarea:focus { outline: 1px solid #438c88; }
  textarea[readonly] { color: #8695a8; }
  .editor-empty { min-height: 330px; padding: 44px 24px; display: flex; flex-direction: column; justify-content: center; align-items: center; text-align: center; }
  .empty-icon { color: #6dcdbf; background: #112c32; border: 1px solid #26545b; border-radius: 12px; font-family: var(--font-mono, monospace); font-size: 22px; padding: 11px 14px; margin-bottom: 18px; }
  .editor-empty h3 { font-size: 19px; font-weight: 550; letter-spacing: -.02em; margin: 0 0 10px; }
  .editor-empty p { color: #8497ae; font-size: 13px; line-height: 1.75; max-width: 380px; margin: 0 0 20px; }
  .setup-link { font-size: 11px; margin-top: 15px; }
  .editor-status { display: flex; align-items: center; justify-content: space-between; padding: 9px 14px; background: #101d2b; border-top: 1px solid #26374b; font-size: 10px; color: #86a0ba; }
  .editor-status > span { display: flex; align-items: center; gap: 7px; }
  .editor-status i { display: block; width: 5px; height: 5px; background: #70beae; border-radius: 50%; }
  .editor-status .unsaved { color: #dec184; }
  .editor-status .unsaved i { background: #dec184; }
  .editor-status > div { display: flex; gap: 20px; }
  .editor-actions { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-top: 14px; }
  .editor-actions > p { color: #6f839c; font-size: 10px; line-height: 1.8; margin: 0; }
  kbd { font-family: inherit; color: #9db0c7; }
  .actions { display: flex; gap: 8px; flex-shrink: 0; }
  .syntax-result { padding: 14px 16px; border: 1px solid #2b5b53; border-radius: 7px; background: #112821; margin-top: 18px; font-size: 12px; }
  .syntax-result strong { color: #84d9ba; font-weight: 550; }
  .syntax-result p { color: #a2bdaf; margin: 5px 0 0; line-height: 1.65; white-space: pre-wrap; overflow-wrap: anywhere; }
  .syntax-error { background: #301d22; border-color: #6c3d48; }
  .syntax-error strong { color: #f2a3a3; }
  .syntax-error p { color: #d6adb4; }
  .workspace-footer { display: flex; gap: 24px; justify-content: space-between; align-items: baseline; margin-top: 22px; padding-top: 17px; border-top: 1px solid #243144; }
  .workspace-footer p { font-size: 11px; color: #7f93aa; line-height: 1.7; margin: 0; max-width: 490px; }
  .workspace-footer a { font-size: 11px; white-space: nowrap; }
  .replace-confirmation { background: #30271b; border: 1px solid #735a35; border-radius: 8px; padding: 16px; margin-bottom: 18px; display: flex; align-items: center; gap: 20px; }
  .replace-confirmation strong { font-size: 12px; font-weight: 600; color: #edcea0; }
  .replace-confirmation p { font-size: 12px; color: #c4b393; line-height: 1.6; margin: 5px 0 0; }
  .replace { background: #d9b47a; color: #2b2111; }
  button:focus-visible, a:focus-visible, select:focus-visible { outline: 2px solid #73d7c4; outline-offset: 3px; }
  @media (max-width: 780px) {
    .strategy-bar { flex-wrap: wrap; gap: 12px; }
    .strategy-select { flex: 1 1 170px; }
    .strategy-bar > p { order: 3; flex-basis: 100%; }
    .editor-actions, .workspace-footer, .replace-confirmation { align-items: flex-start; flex-direction: column; }
    .editor-actions > p { order: 2; }
    .file-path { display: none; }
    .workspace-footer { gap: 10px; }
  }
  @media (max-width: 460px) {
    .workspace-heading { flex-direction: column; gap: 10px; }
    .editor-status > div { gap: 10px; }
    .editor-status > div span:last-child { display: none; }
    .line-numbers { width: 40px; }
    .line-numbers pre { padding-right: 9px; }
    textarea { padding-left: 12px; }
    .code-area { height: 360px; }
    .editor-empty { min-height: 300px; padding: 30px 18px; }
    .file-tab { min-width: 125px; padding: 0 12px; }
  }
</style>
