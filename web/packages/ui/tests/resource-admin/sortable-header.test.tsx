// @vitest-environment jsdom
import { Table, TableBody, TableHeader, TableRow } from '@pivox/primitives/table';
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { SortableHeader } from '../../src/resource-admin/sortable-header';

import type { SortState } from '../../src/resource-admin/types';

function wrap(sort: SortState | null, onToggle: () => void) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <SortableHeader field="displayName" sort={sort} onToggle={onToggle}>
            Name
          </SortableHeader>
        </TableRow>
      </TableHeader>
      <TableBody />
    </Table>
  );
}

function ariaSort(): string | null {
  return screen.getByRole('columnheader').getAttribute('aria-sort');
}

describe('SortableHeader', () => {
  it('reports no sort when the column is inactive', () => {
    render(wrap(null, vi.fn()));
    expect(ariaSort()).toBe('none');
  });

  it('reflects the active direction via aria-sort', () => {
    const { rerender } = render(
      wrap({ field: 'displayName', direction: 'asc' }, vi.fn()),
    );
    expect(ariaSort()).toBe('ascending');

    rerender(wrap({ field: 'displayName', direction: 'desc' }, vi.fn()));
    expect(ariaSort()).toBe('descending');
  });

  it('stays inactive when a different column holds the sort', () => {
    render(wrap({ field: 'updateTime', direction: 'asc' }, vi.fn()));
    expect(ariaSort()).toBe('none');
  });

  it('toggles its own field on click', () => {
    const onToggle = vi.fn();
    render(wrap(null, onToggle));
    fireEvent.click(screen.getByRole('button', { name: 'Name' }));
    expect(onToggle).toHaveBeenCalledWith('displayName');
  });
});
