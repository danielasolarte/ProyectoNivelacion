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

async function sendAuth(path, form) {
  const response = await fetch(`${API}${path}`, { method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({email: form.email.value, password: form.password.value}) });
  const data = await response.json();
  if (!response.ok) throw new Error(data || 'Unable to authenticate');
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
  const data = await response.json();
  if (!response.ok) { $('job-message').textContent = data; return; }
  $('job-card').classList.remove('hidden'); $('job-name').textContent = file.name; $('job-id').textContent = data.job_id; $('download').classList.add('hidden'); poll(data.job_id);
});

async function poll(jobId) {
  clearTimeout(pollTimer);
  const response = await fetch(`${API}/jobs/${jobId}`, {headers: {Authorization: `Bearer ${token}`} });
  if (!response.ok) return;
  const job = await response.json(); $('job-status').textContent = job.status; $('job-message').textContent = job.error || (job.status === 'completed' ? 'Bundle validated and ready.' : 'Worker is processing the document...');
  $('progress-bar').style.width = job.status === 'completed' ? '100%' : job.status === 'failed' ? '100%' : job.status === 'processing' ? '65%' : '30%';
  if (job.status === 'completed') { $('download').href = `${API}/jobs/${jobId}/download`; $('download').setAttribute('download', 'bundle.zip'); $('download').classList.remove('hidden'); return; }
  if (job.status !== 'failed') pollTimer = setTimeout(() => poll(jobId), 800);
}

fetch(`${API}/health`).then((response) => { if (response.ok) $('connection').textContent = 'Online'; }).catch(() => {});
showWorkspace();