const API = 'http://localhost:8080';
const tokenKey = 'okf_token';
let token = localStorage.getItem(tokenKey);
let pollTimer;

const $ = (id) => document.getElementById(id);
const authPanel = $('auth-panel');
const workspace = $('workspace');
const authMessage = $('auth-message');

function showWorkspace() {
  authPanel.classList.toggle('hidden', Boolean(token));
  workspace.classList.toggle('hidden', !token);
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
  localStorage.setItem(tokenKey, token);
  authMessage.textContent = '';
  showWorkspace();
}

$('login-form').addEventListener('submit', async (event) => { event.preventDefault(); try { await sendAuth('/auth/login', {email: $('login-email'), password: $('login-password')}); } catch (error) { authMessage.textContent = error.message; } });
$('register-form').addEventListener('submit', async (event) => { event.preventDefault(); try { await sendAuth('/auth/register', {email: $('register-email'), password: $('register-password')}); } catch (error) { authMessage.textContent = error.message; } });
$('logout').addEventListener('click', () => { token = null; localStorage.removeItem(tokenKey); showWorkspace(); });

$('document').addEventListener('change', (event) => { $('file-name').textContent = event.target.files[0]?.name || 'Choose a Markdown file'; });
$('upload-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const file = $('document').files[0];
  if (!file) return;
  const form = new FormData(); form.append('document', file);
  const response = await fetch(`${API}/upload`, {method: 'POST', headers: {Authorization: `Bearer ${token}`}, body: form});
  const { data, message } = await readBody(response);
  if (!response.ok) { $('job-message').textContent = message || 'No se pudo subir el documento.'; return; }
  $('job-card').classList.remove('hidden'); $('job-name').textContent = file.name; $('job-id').textContent = data.job_id; $('download').classList.add('hidden'); $('retry').classList.add('hidden'); poll(data.job_id);

async function poll(jobId) {
  clearTimeout(pollTimer);
  const response = await fetch(`${API}/jobs/${jobId}`, {headers: {Authorization: `Bearer ${token}`} });
  if (!response.ok) return;
  const { data: job } = await readBody(response);
  if (!job) return;
  $('job-status').textContent = job.status; $('job-message').textContent = job.error || (job.status === 'completed' ? 'Bundle validated and ready.' : 'Worker is processing the document...');
  $('progress-bar').style.width = job.status === 'completed' ? '100%' : job.status === 'failed' ? '100%' : job.status === 'processing' ? '65%' : '30%';
  if (job.status === 'completed') { $('download').dataset.jobId = jobId; $('download').classList.remove('hidden'); $('retry').classList.add('hidden'); return; }
  if (job.status === 'failed') { $('retry').dataset.jobId = jobId; $('retry').classList.remove('hidden'); return; }
  if (job.status !== 'failed') pollTimer = setTimeout(() => poll(jobId), 800);
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

fetch(`${API}/health`).then((response) => { if (response.ok) $('connection').textContent = 'Online'; }).catch(() => {});
showWorkspace();