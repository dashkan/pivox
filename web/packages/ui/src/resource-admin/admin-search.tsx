'use client';

import { Input } from '@pivox/primitives/input';
import { SearchIcon } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';

/**
 * Search box for a resource-admin list. Shows keystrokes immediately but commits
 * `onChange` only after `debounceMs` of idle, so a URL-backed filter isn't
 * navigated on every keypress. `debounceMs <= 0` commits synchronously.
 */
export function AdminSearch({
  value,
  onChange,
  placeholder,
  debounceMs = 0,
}: {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  debounceMs?: number;
}) {
  const [draft, setDraft] = useState(value);
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);

  // Resync the draft when the committed value changes from outside (nav, clear).
  // An external change supersedes any in-flight debounced commit, so cancel the
  // pending timer — otherwise a stale "type then Clear within debounceMs" timer
  // fires and re-commits the text that was just cleared.
  useEffect(() => {
    setDraft(value);
    clearTimeout(timer.current);
  }, [value]);

  useEffect(() => () => clearTimeout(timer.current), []);

  const handle = (next: string) => {
    setDraft(next);
    if (debounceMs <= 0) {
      onChangeRef.current(next);
      return;
    }
    clearTimeout(timer.current);
    timer.current = setTimeout(() => onChangeRef.current(next), debounceMs);
  };

  return (
    <div className="relative max-w-sm">
      <SearchIcon className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
      {/* Clear the icon (left 2.5 + size 4 ≈ 2.25rem) with room to spare. */}
      <Input
        type="search"
        value={draft}
        onChange={(e) => handle(e.target.value)}
        placeholder={placeholder}
        className="pl-9"
      />
    </div>
  );
}
