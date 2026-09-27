// Runs in the declared Codex functions.exec runtime, never on the gateway.
// Codex PlanHandler accepts explanation + [{step,status}] and emits PlanUpdate.
// Codex code_mode_name_for_tool_name removes the default functions namespace,
// so the native handler's runtime name is exactly update_plan, not a suffix match.
async function bpsClientUpdatePlan(args) {
  const fail = (code, message) => text({ success: false, error: { code, message } });
  const target = ALL_TOOLS.find(tool => tool.name === 'update_plan' && typeof tools[tool.name] === 'function');
  if (!target) {
    return fail('CLIENT_PLAN_TOOL_UNAVAILABLE', 'This client runtime does not expose update_plan. No plan was updated. Continue with the tools actually available in this client.');
  }
  const isObject = value => value !== null && typeof value === 'object' && !Array.isArray(value);
  const invalid = () => fail('CLIENT_PLAN_ARGUMENTS_INVALID', 'Codex update_plan requires plan items with a step or description and status pending, in_progress, or completed, with at most one in_progress step. Put failed or skipped outcomes in the step text or explanation and choose the current actionable status. No plan was updated.');
  if (!isObject(args) || Object.keys(args).some(key => !['summary', 'explanation', 'plan'].includes(key)) || !Array.isArray(args.plan)) return invalid();
  if (args.summary !== undefined && typeof args.summary !== 'string') return invalid();
  if (args.explanation !== undefined && args.explanation !== null && typeof args.explanation !== 'string') return invalid();
  const plan = [];
  for (const item of args.plan) {
    if (!isObject(item) || Object.keys(item).some(key => !['id', 'description', 'step', 'status', 'result'].includes(key))) return invalid();
    if (['id', 'description', 'step', 'result'].some(key => item[key] !== undefined && typeof item[key] !== 'string')) return invalid();
    if (item.step !== undefined && item.description !== undefined && item.step !== item.description) return invalid();
    const step = item.step === undefined ? item.description : item.step;
    if (typeof step !== 'string' || !['pending', 'in_progress', 'completed'].includes(item.status)) return invalid();
    plan.push({ step: item.result ? step + '\nResult: ' + item.result : step, status: item.status });
  }
  if (plan.filter(item => item.status === 'in_progress').length > 1) return invalid();
  const params = { plan };
  const explanation = [args.explanation, args.summary].filter(value => typeof value === 'string' && value.length > 0);
  if (explanation.length) params.explanation = [...new Set(explanation)].join('\n');
  try {
    // Propagate the real handler result, including client mode/permission errors.
    text(await tools[target.name](params));
  } catch {
    fail('CLIENT_PLAN_EXECUTION_FAILED', 'The client update_plan call did not complete successfully. Check the client tool result before updating the plan again.');
  }
}
