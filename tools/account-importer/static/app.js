const csrf = document.querySelector('meta[name="csrf-token"]').content;
const feedback = document.getElementById('feedback');
const tableBody = document.getElementById('jobs-body');
const labels = { importing: '导入中', monitoring: '监控中', invalid: '授权失效', reauthorizing: '重新授权中', attention: '需关注', failed: '失败', completed: '监控结束' };
let reauthJob = null;

function setFeedback(message, error = false) {
  feedback.textContent = message;
  feedback.classList.toggle('error', error);
}

async function request(url, options = {}) {
  const response = await fetch(url, { cache: 'no-store', ...options });
  const body = await response.json();
  if (!response.ok) throw new Error(body.detail || '请求失败');
  return body;
}

function submitOptions(body) {
  return { method: 'POST', headers: { 'X-CSRF-Token': csrf }, body };
}

function tab(mode) {
  for (const name of ['file', 'account']) {
    const active = name === mode;
    document.getElementById('tab-' + name).setAttribute('aria-selected', String(active));
    document.getElementById('panel-' + name).hidden = !active;
  }
  setFeedback('');
}

document.getElementById('tab-file').addEventListener('click', () => tab('file'));
document.getElementById('tab-account').addEventListener('click', () => tab('account'));
document.getElementById('show-account').addEventListener('change', event => {
  document.getElementById('account-line').type = event.target.checked ? 'text' : 'password';
});

async function loadConfig() {
  try {
    const config = await request('api/config');
    document.getElementById('server-name').textContent = config.base_url;
    const reference = document.getElementById('reference-account');
    reference.replaceChildren();
    for (const account of config.accounts || []) {
      const option = document.createElement('option');
      option.value = String(account.id);
      option.textContent = account.name + ' · #' + account.id + (account.status ? ' · ' + account.status : '');
      reference.append(option);
    }
    if (reference.options.length) {
      reference.selectedIndex = 0;
      document.getElementById('file-group').options[0].textContent = '使用所选参照账号设置';
      document.getElementById('account-group').options[0].textContent = '使用所选参照账号设置';
      await request('api/jobs/reference/watch?account_id=' + encodeURIComponent(reference.value), { method: 'POST', headers: { 'X-CSRF-Token': csrf } });
    } else {
      setFeedback('未读取到 OpenAI OAuth 账号；新账号导入需要先选择参照账号。', true);
    }
    for (const group of config.groups) {
      if (group.platform && group.platform !== 'openai') continue;
      for (const id of ['file-group', 'account-group']) {
        const option = document.createElement('option');
        option.value = String(group.id);
        option.textContent = group.name + ' · #' + group.id;
        document.getElementById(id).append(option);
      }
    }
    if (!config.groups.length && !config.accounts.length) {
      setFeedback('未获取到 OpenAI 分组；请检查 Sub2API 管理员接口。', true);
    }
  } catch {
    document.getElementById('server-name').textContent = '连接失败';
    setFeedback('无法连接本地服务。', true);
  }
}

document.getElementById('reference-account').addEventListener('change', async event => {
  try {
    await request('api/jobs/reference/watch?account_id=' + encodeURIComponent(event.target.value), { method: 'POST', headers: { 'X-CSRF-Token': csrf } });
    await loadJobs();
  } catch (error) { setFeedback(error.message, true); }
});

function addCell(row, text, className = '') {
  const cell = row.insertCell();
  cell.textContent = text;
  if (className) cell.className = className;
  return cell;
}

async function loadPlugins() {
  const controls = [
    ['file-plugin', 'refresh-plugins', 'plugin-hint'],
    ['account-plugin', 'refresh-account-plugins', 'account-plugin-hint'],
  ].map(ids => {
    const [select, refresh, hint] = ids.map(id => document.getElementById(id));
    const previous = select.value;
    select.disabled = true;
    refresh.disabled = true;
    return { select, refresh, hint, previous };
  });
  try {
    const plugins = await request('api/plugins');
    for (const { select, hint, previous } of controls) {
      select.replaceChildren(new Option('不添加到插件', ''));
      for (const plugin of plugins) {
        select.add(new Option(plugin.name + ' · v' + plugin.version + ' · 已有 ' + plugin.account_count + ' 个账号', String(plugin.id)));
      }
      if (plugins.some(plugin => String(plugin.id) === previous)) select.value = previous;
      else if (previous) setFeedback('之前选择的插件已不可用，请重新选择后导入。', true);
      select.disabled = !plugins.length;
      hint.textContent = plugins.length ? '只显示运行正常的已启用 OpenAI OAuth 插件；导入后追加账号，保留已有账号。' : '没有可用的已启用插件，仍可正常导入账号。';
    }
  } catch (error) {
    for (const { hint } of controls) hint.textContent = error.message;
  } finally {
    for (const { refresh } of controls) refresh.disabled = false;
  }
}

async function loadJobs() {
  try {
    const jobs = await request('api/jobs');
    tableBody.replaceChildren();
    if (!jobs.length) {
      const row = tableBody.insertRow();
      const cell = addCell(row, '暂无导入任务', 'empty');
      cell.colSpan = 6;
      return;
    }
    for (const job of jobs) {
      const row = tableBody.insertRow();
      const identity = addCell(row, job.name, 'identity');
      if (job.account_id) {
        const id = document.createElement('small');
        id.textContent = 'Sub2API #' + job.account_id;
        identity.append(id);
      }
      const state = row.insertCell();
      const badge = document.createElement('span');
      badge.className = 'badge ' + job.state;
      badge.textContent = labels[job.state] || job.state;
      state.append(badge);
      if (job.message) {
        const detail = document.createElement('small');
        detail.textContent = job.message;
        state.append(detail);
      }
      if (job.plugin_id) {
        const detail = document.createElement('small');
        detail.textContent = job.plugin_name + '：' + (job.plugin_message || '等待绑定');
        if (job.plugin_status === 'failed') detail.style.color = '#b42318';
        state.append(detail);
      }
      const calls = addCell(row, job.current_concurrency == null ? '等待查询' : (job.current_concurrency > 0 ? '调用中 · ' + job.current_concurrency + ' 路' : '空闲'));
      if (job.last_used_at) {
        const last = document.createElement('small');
        last.textContent = '上次使用 ' + new Date(job.last_used_at).toLocaleString('zh-CN');
        calls.append(last);
      }
      addCell(row, job.mode === 'file' ? 'JSON 文件' : job.mode === 'reference' ? '参考账号' : '账号资料');
      addCell(row, job.watch_until ? new Date(job.watch_until).toLocaleString('zh-CN') : '—');
      const action = row.insertCell();
      if (!job.retired && ((job.mode === 'file' && job.account_id) || (job.mode === 'credentials' && job.has_saved_credentials))) {
        action.className = 'job-actions';
        const busy = ['importing', 'reauthorizing'].includes(job.state);
        const button = document.createElement('button');
        button.className = 'text-button';
        button.type = 'button';
        button.disabled = busy;
        button.textContent = job.mode === 'file' ? 'RT 重新授权' : '手动重新授权';
        button.addEventListener('click', async () => {
          if (job.mode === 'file') { openAuthorization(job); return; }
          button.disabled = true;
          setFeedback('正在使用首次导入的账号资料重新授权…');
          try {
            await request('api/jobs/' + encodeURIComponent(job.id) + '/reauthorize/credentials', {
              method: 'POST', headers: { 'X-CSRF-Token': csrf },
            });
            setFeedback('已使用首次导入的资料启动授权，结果会在下方更新。');
            await loadJobs();
          } catch (error) { setFeedback(error.message, true); button.disabled = false; }
        });
        action.append(button);
        if (job.mode === 'file') {
          const upload = document.createElement('button');
          upload.className = 'text-button';
          upload.type = 'button';
          upload.disabled = busy;
          upload.textContent = '重新上传 JSON';
          upload.addEventListener('click', () => { reauthJob = job.id; document.getElementById('reauth-json').click(); });
          action.append(upload);
        } else if (job.attempts) {
          const attempts = document.createElement('small');
          attempts.textContent = '自动重试 ' + job.attempts + '/3 次';
          action.append(attempts);
        }
      } else if (job.mode === 'reference' && ['completed', 'attention'].includes(job.state)) {
        const button = document.createElement('button');
        button.className = 'text-button';
        button.type = 'button';
        button.textContent = '重新监控';
        button.addEventListener('click', async () => {
          try {
            await request('api/jobs/reference/watch?account_id=' + encodeURIComponent(job.account_id), { method: 'POST', headers: { 'X-CSRF-Token': csrf } });
            await loadJobs();
          } catch (error) { setFeedback(error.message, true); }
        });
        action.append(button);
      } else {
        action.textContent = '—';
      }
    }
  } catch {
    setFeedback('任务状态刷新失败。', true);
  }
}

document.getElementById('panel-file').addEventListener('submit', async event => {
  event.preventDefault();
  const form = event.currentTarget;
  const button = form.querySelector('button[type=submit]');
  button.disabled = true;
  setFeedback('正在导入 JSON 文件…');
  const data = new FormData();
  data.append('file', document.getElementById('import-json').files[0]);
  const group = document.getElementById('file-group').value;
  const plugin = document.getElementById('file-plugin').value;
  if (plugin) data.append('plugin_id', plugin);
  if (group) data.append('group_id', group);
  data.append('profile_account_id', document.getElementById('reference-account').value);
  try {
    const jobs = await request('api/import/file', submitOptions(data));
    const pluginFailed = jobs.some(job => job.plugin_status === 'failed');
    setFeedback('已处理 ' + jobs.length + ' 个账号；' + (pluginFailed ? '部分插件绑定未成功，请查看下方详情。' : '请查看下方状态。'), pluginFailed);
    form.reset();
    await loadJobs();
  } catch (error) { setFeedback(error.message, true); }
  finally { button.disabled = false; }
});

document.getElementById('panel-account').addEventListener('submit', async event => {
  event.preventDefault();
  const form = event.currentTarget;
  const button = form.querySelector('button[type=submit]');
  const line = document.getElementById('account-line').value;
  const group = document.getElementById('account-group').value;
  const plugin = document.getElementById('account-plugin').value;
  button.disabled = true;
  setFeedback('正在创建登录任务…');
  try {
    await request('api/import/account?profile_account_id=' + encodeURIComponent(document.getElementById('reference-account').value), { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Content-Type': 'application/json' }, body: JSON.stringify({ account_line: line, group_ids: group ? [Number(group)] : [], plugin_id: plugin ? Number(plugin) : null }) });
    document.getElementById('account-line').value = '';
    setFeedback('登录任务已创建，结果会在下方更新。');
    await loadJobs();
  } catch (error) { setFeedback(error.message, true); }
  finally { button.disabled = false; }
});

document.getElementById('reauth-json').addEventListener('change', async event => {
  const file = event.target.files[0];
  if (!file || !reauthJob) return;
  const data = new FormData();
  data.append('file', file);
  setFeedback('正在应用新的授权文件…');
  try {
    await request('api/jobs/' + encodeURIComponent(reauthJob) + '/reauthorize', submitOptions(data));
    setFeedback('已提交新授权，监控窗口重新开始。');
    await loadJobs();
  } catch (error) { setFeedback(error.message, true); }
  finally { event.target.value = ''; reauthJob = null; }
});

const authorizationDialog = document.getElementById('authorization-dialog');
const authorizationForm = document.getElementById('authorization-form');
const authorizationSecret = document.getElementById('authorization-secret');
const authorizationError = document.getElementById('authorization-error');
let authorizationJob = null;

function openAuthorization(job) {
  authorizationJob = job;
  authorizationForm.reset();
  authorizationError.textContent = '';
  document.getElementById('authorization-account').textContent = job.name + ' · #' + job.account_id;
  authorizationDialog.showModal();
  authorizationSecret.focus();
}

document.getElementById('authorization-cancel').addEventListener('click', () => authorizationDialog.close());
authorizationDialog.addEventListener('close', () => {
  authorizationForm.reset();
  authorizationJob = null;
  authorizationError.textContent = '';
});
authorizationForm.addEventListener('submit', async event => {
  event.preventDefault();
  if (!authorizationJob) return;
  const job = authorizationJob;
  const button = document.getElementById('authorization-submit');
  const body = { refresh_token: authorizationSecret.value };
  button.disabled = true;
  authorizationError.textContent = '';
  // Clear the input immediately; the request body is never persisted in the page.
  authorizationSecret.value = '';
  try {
    await request('api/jobs/' + encodeURIComponent(job.id) + '/reauthorize/refresh-token', {
      method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Content-Type': 'application/json' }, body: JSON.stringify(body),
    });
    authorizationDialog.close();
    setFeedback('手动授权任务已提交，成功后重新监控 20 分钟。');
    await loadJobs();
  } catch (error) {
    if (authorizationDialog.open && authorizationJob?.id === job.id) authorizationError.textContent = error.message;
    else setFeedback(error.message, true);
  } finally { button.disabled = false; }
});

document.getElementById('refresh').addEventListener('click', loadJobs);
document.getElementById('refresh-plugins').addEventListener('click', loadPlugins);
document.getElementById('refresh-account-plugins').addEventListener('click', loadPlugins);
loadPlugins();
loadConfig();
loadJobs();
setInterval(loadJobs, 5000);
