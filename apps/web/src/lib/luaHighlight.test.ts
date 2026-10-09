import { describe, expect, it } from 'vitest';
import { highlightLua } from './luaHighlight';

const strip = (html: string) => html.replace(/<[^>]+>/g, '').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&amp;/g, '&');

describe('highlightLua', () => {
  it('preserves the source text exactly', () => {
    const source = 'local a = "x < y" -- note\nif a ~= nil then return 0x1F end\n--[[ block\ncomment ]] print([[raw]])';
    expect(strip(highlightLua(source))).toBe(source);
  });

  it('classifies keywords, strings, numbers, comments, and SDK calls', () => {
    const html = highlightLua('local x = arena.drive_to(obs, 12.5) -- go\nreturn "ok"');
    expect(html).toContain('<span class="tok-keyword">local</span>');
    expect(html).toContain('<span class="tok-arena">arena</span>');
    expect(html).toContain('<span class="tok-call">drive_to</span>');
    expect(html).toContain('<span class="tok-number">12.5</span>');
    expect(html).toContain('<span class="tok-comment">-- go</span>');
    expect(html).toContain('<span class="tok-string">"ok"</span>');
  });

  it('escapes HTML inside tokens', () => {
    expect(highlightLua('x = "<b>"')).toContain('&lt;b&gt;');
    expect(highlightLua('x = "<b>"')).not.toContain('<b>');
  });

  it('handles unterminated strings and comments without looping', () => {
    expect(strip(highlightLua('s = "open\n--[[ never closed'))).toBe('s = "open\n--[[ never closed');
  });
});
