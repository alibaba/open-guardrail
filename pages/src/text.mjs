import { decodeHTMLStrict } from 'entities';

// This returns text, not safe HTML. Encode it for each output context.
export function inlineText(tokens) {
  return tokens.map(token => {
    if (token.type === 'br') return '\n';
    // Code spans and raw HTML are shown literally by the documentation reader.
    if (token.type === 'codespan' || token.type === 'html') return token.text;
    if (token.tokens) return inlineText(token.tokens);
    return decodeHTMLStrict(token.text || '');
  }).join('');
}

export const escapeHtml = value => String(value).replace(/[&<>"']/g,
  c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
