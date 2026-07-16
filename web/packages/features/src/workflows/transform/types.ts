import type { components } from '@pivox/client';

type Schemas = components['schemas'];

export type WorkflowVersion = Schemas['v1WorkflowVersion'];
export type Sequence = Schemas['v1Sequence'];
export type Step = Schemas['workflowsV1Step'];
export type Activity = Schemas['v1Activity'];
export type Condition = Schemas['v1Condition'];
export type Branch = Schemas['v1Branch'];
export type Parallel = Schemas['v1Parallel'];
export type Try = Schemas['v1Try'];
export type HttpActivity = Schemas['v1HttpActivity'];
export type SetActivity = Schemas['v1SetActivity'];
export type RunWorkflowActivity = Schemas['v1RunWorkflowActivity'];
export type FailActivity = Schemas['v1FailActivity'];
export type EndActivity = Schemas['v1EndActivity'];
