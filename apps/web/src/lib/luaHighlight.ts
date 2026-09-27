// Minimal Lua 5.4 highlighter for the workspace editor. It only classifies
// tokens for colour; it never needs to be a full parser. Output is escaped
// HTML meant for a read-only layer under a transparent textarea.
const KEYWORDS = new Set(['and', 'break', 'do', 'else', 'elseif', 'end', 'false', 'for', 'function', 'goto', 'if', 'in', 'local', 'nil', 'not', 'or', 'repeat', 'return', 'then', 'true', 'until', 'while']);
const BUILTINS = new Set(['assert', 'error', 'ipairs', 'pairs', 'pcall', 'print', 'require', 'select', 'setmetatable', 'tonumber', 'tostring', 'type', 'math', 'string', 'table', 'os', 'io', 'coroutine', 'utf8']);

const escape = (text: string) => text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
const wrap = (kind: string, text: string) => `<span class="tok-${kind}">${escape(text)}</span>`;

export function highlightLua(source: string): string {
  let out = '';
  let i = 0;
  const n = source.length;
  while (i < n) {
    const rest = source.slice(i);
    let match: RegExpMatchArray | null;
    if ((match = rest.match(/^--\[(=*)\[[\s\S]*?(?:\]\1\]|$)/))) out += wrap('comment', match[0]);
    else if ((match = rest.match(/^--[^\n]*/))) out += wrap('comment', match[0]);
    else if ((match = rest.match(/^\[(=*)\[[\s\S]*?(?:\]\1\]|$)/))) out += wrap('string', match[0]);
    else if ((match = rest.match(/^"(?:\\.|[^"\\\n])*"?/)) || (match = rest.match(/^'(?:\\.|[^'\\\n])*'?/))) out += wrap('string', match[0]);
    else if ((match = rest.match(/^(?:0[xX][0-9a-fA-F]+|\d+\.?\d*(?:[eE][+-]?\d+)?|\.\d+)/))) out += wrap('number', match[0]);
    else if ((match = rest.match(/^[A-Za-z_][A-Za-z0-9_]*/))) {
      const word = match[0];
      const prev = source[i - 1];
      if (KEYWORDS.has(word)) out += wrap('keyword', word);
      else if (word === 'arena' || word === 'self' || word === 'obs') out += wrap('arena', word);
      else if (prev === '.' && /^\s*\(/.test(source.slice(i + word.length))) out += wrap('call', word);
      else if (BUILTINS.has(word) && prev !== '.') out += wrap('builtin', word);
      else out += escape(word);
    } else {
      match = [source[i]] as unknown as RegExpMatchArray;
      out += escape(source[i]);
    }
    i += match[0].length;
  }
  return out;
}
