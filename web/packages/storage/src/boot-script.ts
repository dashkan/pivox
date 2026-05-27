/**
 * Pre-hydration inline-script builder.
 *
 * Returns a self-contained JavaScript string for rendering in
 * `<head><script>...</script>`. The script runs synchronously BEFORE
 * the body paints and BEFORE any framework mounts. For each
 * registered storage item, it:
 *   1. Reads the value from the platform's backend — cookie on
 *      http(s) origins (web), localStorage elsewhere (electron).
 *      Same selection rule as `storage.get` in `operations.ts`.
 *   2. Calls the item's `onBoot` (if defined) with the parsed
 *      value, so the item can apply any DOM/runtime state that
 *      MUST exist before React mounts (e.g., theme's dark class).
 *
 * Generic — no item-specific branches. New items that need pre-paint
 * side effects just define `onBoot` on their item; the script picks
 * them up automatically via `allItems()`.
 *
 * Function serialization: each item's `parse` and `onBoot` are
 * included via `Function.prototype.toString()` and inlined as text.
 * They must be self-contained (no closures over module imports, only
 * globals). See `StorageItem.onBoot`'s doc comment.
 *
 * Per-item try/catch isolates failures so one broken `onBoot` doesn't
 * stop the others. Errors are logged to console.error so production
 * issues surface in DevTools / Sentry's console capture.
 */

import { allItems } from './define';

export function buildBootScript(): string {
  // Construct a JavaScript literal array — NOT JSON — because JSON
  // can't carry function values. Each item's parse + onBoot are
  // emitted as JS source via toString(); the inline script invokes
  // them at runtime.
  const items = allItems().map((item) => {
    const parts = [
      `name:${JSON.stringify(item.name)}`,
      `parse:${item.parse.toString()}`,
    ];
    if (item.onBoot) {
      parts.push(`onBoot:${item.onBoot.toString()}`);
    }
    return `{${parts.join(',')}}`;
  });

  // The runtime loop. Branches on `location.protocol` to pick the
  // backend — same logic as operations.ts's `backend()`, just
  // inlined here so the script is self-contained.
  const script = `(function(){try{
var isCookie=typeof location!=='undefined'&&(location.protocol==='http:'||location.protocol==='https:');
var items=[${items.join(',')}];
for(var i=0;i<items.length;i++){
  try{
    var it=items[i];
    var raw=null;
    if(isCookie){
      var ck=document.cookie.split(/;\\s*/);
      for(var j=0;j<ck.length;j++){
        if(ck[j].indexOf(it.name+'=')===0){
          try{raw=decodeURIComponent(ck[j].slice(it.name.length+1));}catch(e){raw=null;}
          break;
        }
      }
    }else{
      try{raw=localStorage.getItem(it.name);}catch(e){raw=null;}
    }
    if(it.onBoot){
      var parsed=raw!==null?it.parse(raw):null;
      it.onBoot(parsed);
    }
  }catch(e){console.error('[pivox-storage-boot] item failed:',items[i]&&items[i].name,e);}
}
}catch(e){console.error('[pivox-storage-boot] outer failure:',e);}})()`;

  // Defensive: prevent any literal `</script>` inside a serialized
  // parse / onBoot body (string literal, regex, preserved comment)
  // from breaking out of the inline <script> tag. The HTML parser
  // ends a script element on the first case-insensitive `</script`
  // sequence regardless of JS context, so escape the slash with a
  // backslash — the resulting JS is identical at runtime, but the
  // HTML tokenizer no longer sees a closing tag. Same defense used
  // by Next.js, Remix, and React Server Components.
  return script.replace(/<\/script/gi, '<\\/script');
}
