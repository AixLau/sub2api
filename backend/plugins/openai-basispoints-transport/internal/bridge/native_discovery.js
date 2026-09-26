// Runs inside the declared Codex functions.exec runtime, never on the gateway.
async function bpsClientDiscovery(config) {
  const args = config.arguments;
  const fail = (code, message) => text({ success: false, error: { code, message } });
  const integer = (value, fallback, min, max) => {
    if (value === undefined) return fallback;
    if (!Number.isSafeInteger(value) || value < min || value > max) throw new Error('invalid_integer');
    return value;
  };
  const fingerprint = value => {
    let hash = 2166136261;
    for (const ch of JSON.stringify(value)) hash = Math.imul(hash ^ ch.charCodeAt(0), 16777619);
    return (hash >>> 0).toString(16);
  };
  const page = (items, size, cursor) => {
    const key = fingerprint(items);
    let offset = 0;
    if (cursor !== undefined && cursor !== '') {
      if (typeof cursor !== 'string' || !cursor.startsWith(key + ':')) throw new Error('stale_cursor');
      const position = cursor.slice(key.length + 1);
      if (!/^(0|[1-9][0-9]*)$/.test(position)) throw new Error('invalid_cursor');
      offset = integer(Number(position), 0, 0, items.length);
    }
    return { items: items.slice(offset, offset + size), total: items.length,
      next_cursor: offset + size < items.length ? key + ':' + (offset + size) : null };
  };
  const enabled = () => ALL_TOOLS.filter(t => typeof t.name === 'string' && typeof tools[t.name] === 'function')
    .map(t => ({ action_ref: t.name, description: t.description }))
    .sort((a, b) => a.action_ref < b.action_ref ? -1 : a.action_ref > b.action_ref ? 1 : 0);
  try {
    if (config.operation === 'list_skills') {
      if (!config.skills_known) return fail('CLIENT_SKILLS_NOT_ADVERTISED', 'This request has no client-owned skill catalog. Use the declared client tools to discover the relevant environment.');
      const result = page(config.skills, integer(args.limit, 8, 1, 1000), args.cursor);
      text({ success: true, source: 'client_skill_catalog', skills: result.items, total: result.total, next_cursor: result.next_cursor,
        read_tool: 'read_skills', note: 'Use the exact id with read_skills, or read the path using the client filesystem tool. This is the catalog supplied by this client, not a gateway filesystem scan.' });
    } else if (config.operation === 'read_skills') {
      if (!config.skills_known) return fail('CLIENT_SKILLS_NOT_ADVERTISED', 'No client-owned skill catalog is available.');
      if (!Array.isArray(args.skill_ids) || !args.skill_ids.length || args.skill_ids.length > 8 || args.skill_ids.some(id => typeof id !== 'string')) throw new Error('invalid_skill_ids');
      const mode = args.mode === undefined ? 'snippet' : args.mode;
      if (mode !== 'snippet' && mode !== 'full') throw new Error('invalid_mode');
      const offset = integer(args.offset, 0, 0, 10000000);
      const size = mode === 'full' ? 12000 : 4000;
      const files = args.file_paths === undefined || (Array.isArray(args.file_paths) && args.file_paths.length === 0) ? ['SKILL.md'] : args.file_paths;
      if (!Array.isArray(files) || files.length > 8 || files.some(f => typeof f !== 'string' || !f || /[:\\\x00-\x1f]/.test(f) || f.startsWith('/') || f.split('/').some(p => p === '..' || p === '.'))) throw new Error('invalid_file_paths');
      const executor = ALL_TOOLS.find(t => t.name === 'exec_command' && typeof tools[t.name] === 'function');
      if (!executor) return fail('CLIENT_FILE_READER_UNAVAILABLE', 'The client runtime does not expose exec_command. Read the returned skill paths through another declared client tool.');
      const requested = args.skill_ids.map(id => config.skills.find(skill => skill.id === id));
      if (requested.some(skill => !skill)) return fail('UNKNOWN_CLIENT_SKILL', 'Use an exact id returned by list_skills.');
      for (const skill of requested) {
        for (const file of files) {
          const path = file === 'SKILL.md' ? skill.path : skill.path.slice(0, skill.path.lastIndexOf('/') + 1) + file;
          const windows = /^[A-Za-z]:\//.test(path);
          const q = String.fromCharCode(39), dq = String.fromCharCode(34);
          const quoted = q + path.replaceAll(q, windows ? q + q : q + dq + q + dq + q) + q;
          const cmd = windows
            ? '$ErrorActionPreference = ' + q + 'Stop' + q + '; $s = [System.IO.File]::ReadAllText(' + quoted + '); if (' + offset + ' -lt $s.Length) { [Console]::Write($s.Substring(' + offset + ', [Math]::Min(' + size + ', $s.Length - ' + offset + '))) }'
            : 'LC_ALL=C dd if=' + quoted + ' bs=1 skip=' + offset + ' count=' + size;
          const params = { cmd, max_output_tokens: 18000, yield_time_ms: 1000, login: false };
          if (windows) params.shell = 'powershell.exe';
          const result = await tools[executor.name](params);
          text({ skill_id: skill.id, path, source: 'client_filesystem', offset, window: size, offset_unit: windows ? 'utf16_code_units' : 'utf8_bytes',
            continue_at_offset: offset + size, result, note: 'If the returned window is full, continue at the indicated offset. A session_id means the read is still running; use write_stdin before treating the content as complete.' });
        }
      }
    } else if (config.operation === 'list_connectors') {
      const result = page(enabled(), integer(args.page_size, 10, 1, 100), args.cursor);
      text({ success: true, source: 'client_runtime_tools', scope: 'enabled_tools', actions: result.items, total: result.total, next_cursor: result.next_cursor,
        note: 'This lists tools actually enabled in this client runtime. It is not a list of every installed app, connected account, or read-only connector. The description contains each tool declaration and parameter contract. Client permissions still apply.' });
    } else if (config.operation === 'run_connector_action') {
      if (typeof args.action_ref !== 'string' || !args.params || Array.isArray(args.params) || typeof args.params !== 'object') throw new Error('invalid_action');
      if (!enabled().some(t => t.action_ref === args.action_ref)) return fail('UNKNOWN_CLIENT_ACTION', 'Select an exact action_ref from list_connectors.');
      const result = await tools[args.action_ref](args.params);
      if (result && Array.isArray(result.content)) {
        for (const part of result.content) {
          if (part.type === 'image') image(part);
          else if (part.type === 'audio') audio(part);
          else text(part.type === 'text' ? part.text : part);
        }
        if (result.isError) text({ isError: true });
        if (result.structuredContent !== undefined) text(result.structuredContent);
      } else text(result);
    }
  } catch (error) {
    const known = new Set(['invalid_integer', 'invalid_cursor', 'stale_cursor', 'invalid_skill_ids', 'invalid_mode', 'invalid_file_paths', 'invalid_action']);
    fail(known.has(error.message) ? error.message : 'CLIENT_DISCOVERY_EXECUTION_FAILED', 'The client operation did not complete. Check the selected tool contract or runtime error.');
  }
}
