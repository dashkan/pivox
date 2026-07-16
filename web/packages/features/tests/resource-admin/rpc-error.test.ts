import { describe, expect, it } from 'vitest';

import {
  describeRpcError,
  isFailedPrecondition,
  mapDeleteError,
} from '@/resource-admin/rpc-error';

describe('isFailedPrecondition', () => {
  it('is true only for google.rpc.Code 9', () => {
    expect(isFailedPrecondition({ code: 9 })).toBe(true);
    expect(isFailedPrecondition({ code: 5 })).toBe(false);
    expect(isFailedPrecondition(undefined)).toBe(false);
    expect(isFailedPrecondition({})).toBe(false);
  });
});

describe('describeRpcError', () => {
  it('returns the server message when present', () => {
    expect(describeRpcError({ message: 'boom' }, 'fallback')).toBe('boom');
  });

  it('falls back on empty/whitespace/absent message', () => {
    expect(describeRpcError({ message: '   ' }, 'fallback')).toBe('fallback');
    expect(describeRpcError({}, 'fallback')).toBe('fallback');
    expect(describeRpcError(undefined, 'fallback')).toBe('fallback');
  });
});

describe('mapDeleteError', () => {
  it('surfaces the referencing-connectors message on FAILED_PRECONDITION', () => {
    const message =
      'secret is referenced by connector(s) stripe, github; remove the reference before deleting';
    expect(mapDeleteError({ code: 9, message })).toBe(message);
  });

  it('uses the in-use fallback when a FAILED_PRECONDITION carries no message', () => {
    expect(mapDeleteError({ code: 9 })).toBe(
      'Still in use — remove the references before deleting.',
    );
  });

  it('uses the generic fallback for other codes', () => {
    expect(mapDeleteError({ code: 13, message: '' })).toBe(
      "Couldn't delete. Please try again.",
    );
    expect(mapDeleteError(undefined)).toBe(
      "Couldn't delete. Please try again.",
    );
  });
});
