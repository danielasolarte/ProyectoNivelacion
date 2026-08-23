const API = 'http://localhost:8080';
const tokenKey = 'okf_token';
const adminKey = 'okf_is_admin';
let token = localStorage.getItem(tokenKey);
let isAdmin = localStorage.getItem(adminKey) === 'true';
let pollTimer;

const $ = (id) => document.getElementById(id);
const authContainer = $('auth-container') || document.querySelector('.auth-container');
const workspace = $('workspace');
const authMessage = $('auth-message');

function showWorkspace() {
  if (authContainer) authContainer.classList.toggle('hidden', Boolean(token));
  workspace.classList.toggle('hidden', !token);
  if (token) loadBundles();
  $('admin-panel').classList.add('hidden');
  if (token) loadAdminMetrics(true);
}

function toggleAuthForm(showRegister = false) {
  const loginForm = $('login-form');
  const registerForm = $('register-form');
  if (showRegister) {
    loginForm.classList.add('hidden');
    registerForm.classList.remove('hidden');
  } else {
    loginForm.classList.remove('hidden');
    registerForm.classList.add('hidden');
  }
}

function describeFile(file) {
  const name = file?.name || '';
  const extension = name.includes('.') ? name.split('.').pop().toLowerCase() : '';
  if (extension === 'md' || extension === 'markdown') return 'Markdown file';
  if (extension === 'txt') return 'Plain text file';
  return 'Document file';
}

function formatStatus(status) {
  if (!status) return 'Queued';
  return status.split('_').map((part) => part.charAt(0).toUpperCase() + part.slice(1)).join(' ');
}

function showJobState({ name, id = '', status = 'Queued', message = '', showProgress = true }) {
  $('job-card').classList.remove('hidden');
  $('job-name').textContent = name || 'No document selected';
  $('job-id').textContent = id;
  $('latest-bundle-id').textContent = '';
  $('validation-status').textContent = '';
  $('validation-summary').textContent = '';
  $('validation-summary').classList.add('hidden');
  $('job-status').textContent = formatStatus(status);
  $('job-message').textContent = message;
  $('download').classList.add('hidden');
  $('retry').classList.add('hidden');
  $('cancel-job').classList.add('hidden');
  $('new-document').classList.add('hidden');
  document.querySelector('.job-details').open = false;
  $('progress-bar').parentElement.classList.toggle('hidden', !showProgress);
}

function resetDocumentSelection(openPicker = false) {
  clearTimeout(pollTimer);
  $('document').value = '';
  $('file-name').textContent = 'Choose a Markdown file';
  $('file-type').textContent = 'Markdown file';
  $('job-card').classList.add('hidden');
  $('job-message').textContent = '';
  $('latest-bundle-id').textContent = '';
  $('validation-status').textContent = '';
  $('validation-summary').textContent = '';
  $('validation-summary').classList.add('hidden');
  $('download').classList.add('hidden');
  $('retry').classList.add('hidden');
  $('cancel-job').classList.add('hidden');
  $('new-document').classList.add('hidden');
  if (openPicker) $('document').click();
}

function shortId(id) {
  return id ? `${id.slice(0, 8)}...` : '';
}

function formatDate(value) {
  if (!value) return '';
  return new Date(value).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' });
}

function escapeHtml(value) {
  return String(value).replace(/[&<>"']/g, (char) => ({
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;',
  })[char]);
}

function renderBundles(bundles) {
  const list = $('bundle-list');
  list.innerHTML = '';
  if (!bundles.length) {
    $('bundle-message').textContent = 'No bundles yet.';
    return;
  }
  $('bundle-message').textContent = `${bundles.length} bundle${bundles.length === 1 ? '' : 's'} available.`;
  for (const bundle of bundles) {
    const row = document.createElement('article');
    row.className = 'bundle-row';
    row.dataset.bundleId = bundle.bundle_id;
    row.innerHTML = `
      <div>
        <p class="bundle-title">${escapeHtml(bundle.original_name)}</p>
        <p class="bundle-meta">Bundle ID <button type="button" class="bundle-id-button" data-action="lookup" data-bundle-id="${bundle.bundle_id}">${shortId(bundle.bundle_id)}</button> · ${formatStatus(bundle.validation_status)} · ${formatDate(bundle.created_at)}</p>
      </div>
      <div class="bundle-row-actions">
        <button type="button" class="download ghost" data-action="details" data-bundle-id="${bundle.bundle_id}">Details</button>
        <button type="button" class="download" data-action="download" data-bundle-id="${bundle.bundle_id}">Download</button>
      </div>`;
    list.appendChild(row);
  }
}

async function loadBundles() {
  if (!token) return;
  const response = await fetch(`${API}/bundles`, { headers: { Authorization: `Bearer ${token}` } });
  const { data, message } = await readBody(response);
  if (!response.ok || !data) {
    $('bundle-message').textContent = message || 'Could not load your bundles.';
    return;
  }
  renderBundles(data.bundles || []);
}

async function showBundleDetails(bundleId) {
  if (!token || !bundleId) return;
  const response = await fetch(`${API}/bundles/${bundleId}`, { headers: { Authorization: `Bearer ${token}` } });
  const { data, message } = await readBody(response);
  if (!response.ok || !data) {
    $('bundle-message').textContent = message || 'Bundle not found for this account.';
    return;
  }
  $('bundle-id-input').value = data.bundle_id;
  $('bundle-message').textContent = `${data.original_name} · ${formatStatus(data.validation_status)} · job ${shortId(data.job_id)}`;
}

async function downloadBundle(bundleId) {
  if (!token || !bundleId) return;
  const response = await fetch(`${API}/bundles/${bundleId}/download`, { headers: { Authorization: `Bearer ${token}` } });
  if (!response.ok) {
    $('bundle-message').textContent = 'Could not download this bundle.';
    return;
  }
  const file = await response.blob();
  const url = URL.createObjectURL(file);
  const link = document.createElement('a');
  link.href = url;
  link.download = `bundle-${shortId(bundleId).replace('...', '')}.zip`;
  link.click();
  URL.revokeObjectURL(url);
}

const statusLabels = {
  completed: 'Completed',
  failed: 'Failed',
  cancelled: 'Cancelled',
  queued: 'Queued',
  processing: 'Processing',
  valid: 'Valid',
  valid_with_warnings: 'With warnings',
  invalid: 'Invalid',
};

function labelFor(value) {
  return statusLabels[value] || formatStatus(value);
}

function valueTotal(values) {
  return Object.values(values || {}).reduce((sum, value) => sum + Number(value || 0), 0);
}

function pct(value, total) {
  if (!total) return 0;
  return Math.round((Number(value || 0) / total) * 100);
}

function renderMetricCards(data) {
  const jobs = data.jobs_by_status || {};
  const validations = data.bundles_by_validation || {};
  const totalJobs = valueTotal(jobs);
  const totalBundles = valueTotal(validations);
  const completed = Number(jobs.completed || 0);
  const warnings = Number(validations.valid_with_warnings || 0);
  const avg = data.avg_processing_seconds == null ? 'n/a' : `${Number(data.avg_processing_seconds).toFixed(3)}s`;
  $('metric-cards').innerHTML = `
    <div><strong>${totalJobs}</strong><span>Total jobs</span></div>
    <div><strong>${pct(completed, totalJobs)}%</strong><span>Completed</span></div>
    <div><strong>${totalBundles}</strong><span>Published bundles</span></div>
    <div><strong>${warnings}</strong><span>With warnings</span></div>
    <div><strong>${avg}</strong><span>Avg. processing</span></div>`;
}

function renderJobDistribution(elementId, values) {
  const container = $(elementId);
  container.innerHTML = '';
  const order = ['completed', 'failed', 'cancelled', 'processing', 'queued'];
  const entries = order.map((key) => [key, Number((values || {})[key] || 0)]).filter(([, value]) => value > 0);
  const total = valueTotal(values);
  if (!total) {
    container.innerHTML = '<p class="history-message">No data yet.</p>';
    return;
  }
  const stacked = document.createElement('div');
  stacked.className = 'stacked-bar';
  for (const [label, value] of entries) {
    const segment = document.createElement('span');
    segment.className = `stack-segment status-${label}`;
    segment.style.width = `${Math.max((value / total) * 100, 5)}%`;
    segment.title = `${labelFor(label)}: ${value} (${pct(value, total)}%)`;
    stacked.appendChild(segment);
  }
  container.appendChild(stacked);
  for (const [label, value] of entries) {
    const row = document.createElement('div');
    row.className = 'bar-row';
    const width = Math.max((value / total) * 100, 7);
    row.innerHTML = `
      <span><i class="legend-dot status-${label}"></i>${labelFor(label)}</span>
      <span class="bar-track status-${label}" style="--bar-width: ${width}%"><span class="bar-fill"></span></span>
      <strong>${value} · ${pct(value, total)}%</strong>`;
    container.appendChild(row);
  }
}

function renderValidationDonut(elementId, values) {
  const container = $(elementId);
  const entries = Object.entries(values || {}).filter(([, value]) => Number(value) > 0);
  const total = valueTotal(values);
  if (!total) {
    container.innerHTML = '<p class="history-message">No bundles yet.</p>';
    return;
  }
  let cursor = 0;
  const colors = {
    valid: '#8A2F46',
    valid_with_warnings: '#B98676',
    invalid: '#887B76',
    pending: '#DDD4CF',
  };
  const slices = entries.map(([label, value]) => {
    const start = cursor;
    cursor += (Number(value) / total) * 100;
    return `${colors[label] || '#887B76'} ${start}% ${cursor}%`;
  }).join(', ');
  const legend = entries.map(([label, value]) => `
    <div class="donut-legend-row">
      <span><i class="legend-dot" style="background:${colors[label] || '#887B76'}"></i>${labelFor(label)}</span>
      <strong>${value} · ${pct(value, total)}%</strong>
    </div>`).join('');
  container.innerHTML = `
    <div class="donut" style="background: conic-gradient(${slices})"><span>${total}</span></div>
    <div class="donut-legend">${legend}</div>`;
}

async function loadAdminMetrics(probe = false) {
  if (!token) return;
  if (!isAdmin && !probe) return;
  const response = await fetch(`${API}/admin/metrics`, { headers: { Authorization: `Bearer ${token}` } });
  const { data, message } = await readBody(response);
  if (!response.ok || !data) {
    if (!probe) $('admin-message').textContent = message || 'Could not load admin metrics.';
    isAdmin = false;
    localStorage.setItem(adminKey, 'false');
    $('admin-panel').classList.add('hidden');
    return;
  }
  isAdmin = true;
  localStorage.setItem(adminKey, 'true');
  $('admin-panel').classList.remove('hidden');
  $('admin-message').textContent = 'System overview for all users.';
  renderMetricCards(data);
  renderJobDistribution('jobs-chart', data.jobs_by_status);
  renderValidationDonut('validation-chart', data.bundles_by_validation);
  const avg = data.avg_processing_seconds == null ? 'n/a' : `${Number(data.avg_processing_seconds).toFixed(3)}s`;
  $('metric-summary').textContent = `Average processing time: ${avg}. Retried jobs: ${data.jobs_retried}.`;
}

async function readBody(response) {
  const text = await response.text();
  try {
    return { data: JSON.parse(text), message: null };
  } catch {
    return { data: null, message: text };
  }
}

async function sendAuth(path, form) {
  const response = await fetch(`${API}${path}`, { method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({email: form.email.value, password: form.password.value}) });
  const { data, message } = await readBody(response);
  if (!response.ok) throw new Error(message || (data && data.error) || 'No se pudo autenticar');
  token = data.token;
  isAdmin = Boolean(data.is_admin);
  localStorage.setItem(tokenKey, token);
  localStorage.setItem(adminKey, String(isAdmin));
  authMessage.textContent = '';
  showWorkspace();
}

$('login-form').addEventListener('submit', async (event) => { event.preventDefault(); try { await sendAuth('/auth/login', {email: $('login-email'), password: $('login-password')}); } catch (error) { authMessage.textContent = error.message; } });
$('register-form').addEventListener('submit', async (event) => { event.preventDefault(); try { await sendAuth('/auth/register', {email: $('register-email'), password: $('register-password')}); } catch (error) { authMessage.textContent = error.message; } });

$('toggle-register').addEventListener('click', (e) => { e.preventDefault(); toggleAuthForm(true); });
$('toggle-login').addEventListener('click', (e) => { e.preventDefault(); toggleAuthForm(false); });

$('logout').addEventListener('click', () => {
  token = null;
  isAdmin = false;
  localStorage.removeItem(tokenKey);
  localStorage.removeItem(adminKey);
  $('bundle-list').innerHTML = '';
  $('bundle-message').textContent = '';
  $('admin-panel').classList.add('hidden');
  showWorkspace();
});

$('document').addEventListener('change', (event) => {
  const file = event.target.files[0];
  $('file-name').textContent = file?.name || 'Choose a Markdown file';
  $('file-type').textContent = file ? describeFile(file) : 'Markdown file';
});
$('upload-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const file = $('document').files[0];
  if (!file) {
    showJobState({ status: 'Waiting', message: 'Choose a Markdown file before creating a bundle.', showProgress: false });
    $('document').click();
    return;
  }

  const submitButton = event.currentTarget.querySelector('button[type="submit"]');
  submitButton.disabled = true;
  submitButton.textContent = 'Creating bundle...';
  showJobState({ name: file.name, status: 'Uploading', message: 'Uploading your document...', showProgress: true });
  $('progress-bar').style.width = '20%';

  try {
    const form = new FormData(); form.append('document', file);
    const response = await fetch(`${API}/upload`, {method: 'POST', headers: {Authorization: `Bearer ${token}`}, body: form});
    const { data, message } = await readBody(response);
    if (!response.ok || !data?.job_id) {
      showJobState({ name: file.name, status: 'Failed', message: message || 'Could not create this bundle.', showProgress: false });
      return;
    }
    showJobState({ name: file.name, id: data.job_id, status: 'Queued', message: 'Your document is queued for conversion.', showProgress: true });
    poll(data.job_id);
  } catch {
    showJobState({ name: file.name, status: 'Failed', message: 'Could not reach the API. Check that the backend is running.', showProgress: false });
  } finally {
    submitButton.disabled = false;
    submitButton.innerHTML = 'Create bundle <span>&rarr;</span>';
  }
});

async function poll(jobId) {
  clearTimeout(pollTimer);
  const response = await fetch(`${API}/jobs/${jobId}`, {headers: {Authorization: `Bearer ${token}`} });
  if (!response.ok) return;
  const { data: job } = await readBody(response);
  if (!job) return;
  const validationStatus = job.validation_status || '';
  $('job-status').textContent = formatStatus(job.status);
  $('latest-bundle-id').textContent = job.bundle_id ? `Bundle ID: ${job.bundle_id}` : '';
  $('validation-status').textContent = validationStatus ? `Validation: ${formatStatus(validationStatus)}` : '';
  if (validationStatus) {
    $('validation-summary').innerHTML = `<strong>Validation:</strong> ${formatStatus(validationStatus)}`;
    $('validation-summary').classList.remove('hidden');
  } else {
    $('validation-summary').textContent = '';
    $('validation-summary').classList.add('hidden');
  }
  $('job-message').textContent = job.error || (job.status === 'completed' ? (validationStatus === 'valid_with_warnings' ? 'Your bundle is ready to download, with validation warnings noted in the log.' : 'Your bundle is ready to download.') : job.status === 'cancelled' ? 'This conversion was cancelled.' : 'Worker is processing the document...');
  const progress = $('progress-bar').parentElement;
  progress.classList.toggle('hidden', job.status === 'completed');
  $('progress-bar').style.width = job.status === 'completed' ? '100%' : (job.status === 'failed' || job.status === 'cancelled') ? '100%' : job.status === 'processing' ? '65%' : '30%';
  const cancelable = job.status === 'queued' || job.status === 'processing';
  $('cancel-job').classList.toggle('hidden', !cancelable);
  $('cancel-job').dataset.jobId = jobId;
  if (job.status === 'completed') { $('download').dataset.jobId = jobId; $('download').classList.remove('hidden'); $('new-document').classList.remove('hidden'); $('retry').classList.add('hidden'); loadBundles(); return; }
  if (job.status === 'failed' || job.status === 'cancelled') { $('retry').dataset.jobId = jobId; $('retry').classList.remove('hidden'); $('new-document').classList.remove('hidden'); return; }
  pollTimer = setTimeout(() => poll(jobId), 800);

}

$('download').addEventListener('click', async (event) => {
  event.preventDefault();
  event.stopPropagation();
  const jobId = event.currentTarget.dataset.jobId;
  if (!token || !jobId) return;
  const response = await fetch(`${API}/jobs/${jobId}/download`, {headers: {Authorization: `Bearer ${token}`} });
  if (!response.ok) { $('job-message').textContent = 'Could not download this bundle.'; return; }
  const file = await response.blob();
  const url = URL.createObjectURL(file);
  const link = document.createElement('a');
  link.href = url;
  link.download = 'bundle.zip';
  link.click();
  URL.revokeObjectURL(url);
});

$('retry').addEventListener('click', async () => {
  const jobId = $('retry').dataset.jobId;
  if (!token || !jobId) return;
  const response = await fetch(`${API}/jobs/${jobId}/retry`, { method: 'POST', headers: { Authorization: `Bearer ${token}` } });
  const { data, message } = await readBody(response);
  if (!response.ok || !data) { $('job-message').textContent = message || 'No se pudo reintentar el trabajo.'; return; }
  $('retry').classList.add('hidden');
  $('job-id').textContent = data.job_id;
  poll(data.job_id);
});

$('cancel-job').addEventListener('click', async () => {
  const jobId = $('cancel-job').dataset.jobId;
  if (!token || !jobId) return;
  const response = await fetch(`${API}/jobs/${jobId}/cancel`, { method: 'POST', headers: { Authorization: `Bearer ${token}` } });
  const { message } = await readBody(response);
  if (!response.ok) { $('job-message').textContent = message || 'No se pudo cancelar el trabajo.'; return; }
  poll(jobId);
});

$('new-document').addEventListener('click', () => resetDocumentSelection(true));

$('refresh-bundles').addEventListener('click', () => loadBundles());

$('refresh-metrics').addEventListener('click', () => loadAdminMetrics());

$('bundle-lookup').addEventListener('submit', async (event) => {
  event.preventDefault();
  const bundleId = $('bundle-id-input').value.trim();
  if (!bundleId) {
    $('bundle-message').textContent = 'Paste a bundle ID to search.';
    return;
  }
  await showBundleDetails(bundleId);
});

$('bundle-list').addEventListener('click', async (event) => {
  const action = event.target.dataset.action;
  const bundleId = event.target.dataset.bundleId;
  if (!action || !bundleId) return;
  if (action === 'download') await downloadBundle(bundleId);
  if (action === 'details' || action === 'lookup') await showBundleDetails(bundleId);
});

fetch(`${API}/health`).then((response) => { if (response.ok) $('connection').textContent = 'Online'; }).catch(() => {});
showWorkspace();
