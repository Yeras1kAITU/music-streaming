// API Configuration
const API_URL = 'http://localhost:8080';
let authToken = localStorage.getItem('token');
let currentPage = 'home';
let currentTrack = null;
let audioElement = null;
let currentPlaylistId = null;

// Initialize app
document.addEventListener('DOMContentLoaded', () => {
    updateAuthUI();
    loadPricingPlans();
    if (authToken) {
        loadRecommendations();
        loadUserPlaylists();
    }
    setInterval(checkServiceHealth, 30000);
});

// Service Health Check
async function checkServiceHealth() {
    try {
        const response = await fetch(`${API_URL}/health`);
        if (response.ok) {
            console.log('All services healthy');
        }
    } catch (error) {
        console.error('Service health check failed:', error);
    }
}

// Page Navigation
function showPage(page) {
    document.querySelectorAll('.page').forEach(p => p.classList.remove('active'));
    document.getElementById(`${page}Page`).classList.add('active');
    currentPage = page;

    if (page === 'library' && authToken) {
        loadTracks(1);
    } else if (page === 'playlists' && authToken) {
        loadPlaylists();
    } else if (page === 'home') {
        if (authToken) {
            loadRecommendations();
        }
    }
}

// Authentication
function showLogin() {
    document.getElementById('loginModal').style.display = 'block';
}

function showRegister() {
    document.getElementById('registerModal').style.display = 'block';
}

async function login() {
    const email = document.getElementById('loginEmail').value;
    const password = document.getElementById('loginPassword').value;

    try {
        const response = await fetch(`${API_URL}/auth/login`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ email, password })
        });

        if (response.ok) {
            const data = await response.json();
            authToken = data.token;
            localStorage.setItem('token', authToken);
            updateAuthUI();
            closeModal('loginModal');
            showPage('home');
            loadRecommendations();
            showNotification('Login successful!', 'success');
        } else {
            const error = await response.json();
            showNotification(error.error || 'Login failed', 'error');
        }
    } catch (error) {
        console.error('Login error:', error);
        showNotification('Login failed', 'error');
    }
}

async function register() {
    const username = document.getElementById('regUsername').value;
    const email = document.getElementById('regEmail').value;
    const password = document.getElementById('regPassword').value;

    try {
        const response = await fetch(`${API_URL}/auth/register`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ username, email, password })
        });

        if (response.ok) {
            showNotification('Registration successful! Please login.', 'success');
            closeModal('registerModal');
            showLogin();
        } else {
            const error = await response.json();
            showNotification(error.error || 'Registration failed', 'error');
        }
    } catch (error) {
        console.error('Registration error:', error);
        showNotification('Registration failed', 'error');
    }
}

async function logout() {
    if (authToken) {
        try {
            await fetch(`${API_URL}/api/user/logout`, {
                method: 'POST',
                headers: { 'Authorization': `Bearer ${authToken}` }
            });
        } catch (error) {
            console.error('Logout error:', error);
        }
    }
    authToken = null;
    localStorage.removeItem('token');
    updateAuthUI();
    showPage('home');
    showNotification('Logged out successfully', 'info');
}

function updateAuthUI() {
    const navAuth = document.getElementById('navAuth');
    const navUser = document.getElementById('navUser');
    const userName = document.getElementById('userName');

    if (authToken) {
        navAuth.style.display = 'none';
        navUser.style.display = 'flex';
        // Decode JWT to get username (simplified)
        try {
            const payload = JSON.parse(atob(authToken.split('.')[1]));
            userName.textContent = payload.username || 'User';
        } catch (e) {
            userName.textContent = 'User';
        }
    } else {
        navAuth.style.display = 'flex';
        navUser.style.display = 'none';
    }
}

// Tracks Management
let currentPageNum = 1;
let totalPages = 1;

async function loadTracks(page = 1) {
    if (!authToken) return;

    currentPageNum = page;
    try {
        const response = await fetch(`${API_URL}/api/tracks?page=${page}&page_size=20`, {
            headers: { 'Authorization': `Bearer ${authToken}` }
        });

        if (response.ok) {
            const data = await response.json();
            displayTracks(data.tracks || []);
            totalPages = Math.ceil((data.total || 0) / 20);
            updatePagination();
        }
    } catch (error) {
        console.error('Load tracks error:', error);
        showNotification('Failed to load tracks', 'error');
    }
}

function displayTracks(tracks) {
    const grid = document.getElementById('tracksGrid');
    if (!tracks.length) {
        grid.innerHTML = '<p class="empty-state">No tracks found. Upload some music!</p>';
        return;
    }

    grid.innerHTML = tracks.map(track => `
        <div class="track-card">
            <div class="track-cover">🎵</div>
            <div class="track-info">
                <h4>${escapeHtml(track.title)}</h4>
                <p>${escapeHtml(track.artist)}</p>
                <p class="track-meta">${track.plays || 0} plays</p>
            </div>
            <div class="track-actions">
                <button onclick="playTrack('${track.id}', '${escapeHtml(track.title)} - ${escapeHtml(track.artist)}')" class="btn-small">▶ Play</button>
                <button onclick="showAddToPlaylist('${track.id}')" class="btn-small">+ Playlist</button>
                <button onclick="likeTrack('${track.id}')" class="btn-small">❤️ ${track.likes || 0}</button>
            </div>
        </div>
    `).join('');
}

async function searchTracks() {
    const query = document.getElementById('searchInput').value;
    if (!query.trim()) {
        loadTracks(1);
        return;
    }

    try {
        const response = await fetch(`${API_URL}/api/tracks/search?q=${encodeURIComponent(query)}&page=1&page_size=50`, {
            headers: { 'Authorization': `Bearer ${authToken}` }
        });

        if (response.ok) {
            const data = await response.json();
            displayTracks(data.tracks || []);
        }
    } catch (error) {
        console.error('Search error:', error);
    }
}

function updatePagination() {
    const pagination = document.getElementById('pagination');
    if (totalPages <= 1) {
        pagination.innerHTML = '';
        return;
    }

    let html = '<div class="pagination-controls">';
    if (currentPageNum > 1) {
        html += `<button onclick="loadTracks(${currentPageNum - 1})">Previous</button>`;
    }
    html += `<span>Page ${currentPageNum} of ${totalPages}</span>`;
    if (currentPageNum < totalPages) {
        html += `<button onclick="loadTracks(${currentPageNum + 1})">Next</button>`;
    }
    html += '</div>';
    pagination.innerHTML = html;
}

async function uploadTrack() {
    const title = document.getElementById('trackTitle').value;
    const artist = document.getElementById('trackArtist').value;
    const album = document.getElementById('trackAlbum').value;
    const genre = document.getElementById('trackGenre').value;
    const file = document.getElementById('trackFile').files[0];

    if (!title || !artist || !file) {
        showNotification('Please fill all fields', 'error');
        return;
    }

    const formData = new FormData();
    formData.append('title', title);
    formData.append('artist', artist);
    formData.append('album', album);
    formData.append('genre', genre);
    formData.append('audio', file);

    try {
        const response = await fetch(`${API_URL}/api/tracks/upload`, {
            method: 'POST',
            headers: { 'Authorization': `Bearer ${authToken}` },
            body: formData
        });

        if (response.ok) {
            const data = await response.json();
            showNotification('Track uploaded successfully!', 'success');
            closeModal('uploadModal');
            loadTracks(1);
            clearUploadForm();
        } else {
            const error = await response.json();
            showNotification(error.error || 'Upload failed', 'error');
        }
    } catch (error) {
        console.error('Upload error:', error);
        showNotification('Upload failed', 'error');
    }
}

function clearUploadForm() {
    document.getElementById('trackTitle').value = '';
    document.getElementById('trackArtist').value = '';
    document.getElementById('trackAlbum').value = '';
    document.getElementById('trackFile').value = '';
}

// Playlists Management
async function loadPlaylists() {
    if (!authToken) return;

    try {
        const response = await fetch(`${API_URL}/api/playlists`, {
            headers: { 'Authorization': `Bearer ${authToken}` }
        });

        if (response.ok) {
            const data = await response.json();
            displayPlaylists(data.playlists || []);
        }
    } catch (error) {
        console.error('Load playlists error:', error);
    }
}

function displayPlaylists(playlists) {
    const grid = document.getElementById('playlistsGrid');
    if (!playlists.length) {
        grid.innerHTML = '<p class="empty-state">No playlists yet. Create your first playlist!</p>';
        return;
    }

    grid.innerHTML = playlists.map(playlist => `
        <div class="playlist-card" onclick="viewPlaylist('${playlist.id}')">
            <div class="playlist-cover">📁</div>
            <div class="playlist-info">
                <h4>${escapeHtml(playlist.name)}</h4>
                <p>${playlist.track_count || 0} tracks</p>
                ${playlist.is_public ? '<span class="badge">Public</span>' : '<span class="badge">Private</span>'}
            </div>
        </div>
    `).join('');
}

async function viewPlaylist(playlistId) {

    try {

        const response = await fetch(`${API_URL}/api/playlists/${playlistId}`, {
            headers: {
                'Authorization': `Bearer ${authToken}`
            }
        });

        if (!response.ok) {
            showNotification('Failed to load playlist', 'error');
            return;
        }

        const playlist = await response.json();

        const tracks = playlist.tracks || [];

        document.querySelectorAll('.page').forEach(p => {
            p.classList.remove('active');
        });

        document.getElementById('libraryPage').classList.add('active');

        currentPage = 'playlist';

        const grid = document.getElementById('tracksGrid');

        if (!tracks.length) {

            grid.innerHTML = `
                <div class="playlist-view">
                    <h2>${escapeHtml(playlist.name)}</h2>
                    <p>${escapeHtml(playlist.description || '')}</p>
                    <p class="empty-state">Playlist is empty</p>
                </div>
            `;

            return;
        }

        grid.innerHTML = `
            <div class="playlist-view">
                <h2>${escapeHtml(playlist.name)}</h2>

                <p>${escapeHtml(playlist.description || '')}</p>

                <div class="playlist-tracks">

                    ${tracks.map(track => `
                        <div class="track-card">

                            <div class="track-cover">🎵</div>

                            <div class="track-info">
                                <h4>${escapeHtml(track.title)}</h4>
                                <p>${escapeHtml(track.artist)}</p>
                            </div>

                            <div class="track-actions">
                                <button
                                    onclick="playTrack(
                                        '${track.id}',
                                        '${escapeHtml(track.title)} - ${escapeHtml(track.artist)}'
                                    )"
                                    class="btn-small"
                                >
                                    ▶ Play
                                </button>
                            </div>

                        </div>
                    `).join('')}

                </div>
            </div>
        `;

    } catch (error) {

        console.error('View playlist error:', error);

        showNotification('Failed to open playlist', 'error');
    }
}

async function createPlaylist() {
    const name = document.getElementById('playlistName').value;
    const description = document.getElementById('playlistDesc').value;
    const isPublic = document.getElementById('playlistPublic').checked;

    if (!name) {
        showNotification('Please enter a playlist name', 'error');
        return;
    }

    try {
        const response = await fetch(`${API_URL}/api/playlists`, {
            method: 'POST',
            headers: {
                'Authorization': `Bearer ${authToken}`,
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ name, description, is_public: isPublic })
        });

        if (response.ok) {
            showNotification('Playlist created!', 'success');
            closeModal('playlistModal');
            loadPlaylists();
            clearPlaylistForm();
        } else {
            const error = await response.json();
            showNotification(error.error || 'Failed to create playlist', 'error');
        }
    } catch (error) {
        console.error('Create playlist error:', error);
        showNotification('Failed to create playlist', 'error');
    }
}

function clearPlaylistForm() {
    document.getElementById('playlistName').value = '';
    document.getElementById('playlistDesc').value = '';
    document.getElementById('playlistPublic').checked = false;
}

let selectedTrackId = null;

function showAddToPlaylist(trackId) {
    selectedTrackId = trackId;
    const select = document.getElementById('playlistSelect');
    select.innerHTML = '<option value="">Loading...</option>';
    document.getElementById('addToPlaylistModal').style.display = 'block';

    loadUserPlaylistsForSelect();
}

async function loadUserPlaylistsForSelect() {
    try {
        const response = await fetch(`${API_URL}/api/playlists`, {
            headers: { 'Authorization': `Bearer ${authToken}` }
        });

        if (response.ok) {
            const data = await response.json();
            const select = document.getElementById('playlistSelect');
            select.innerHTML = '<option value="">Select playlist</option>' +
                (data.playlists || []).map(p => `<option value="${p.id}">${escapeHtml(p.name)}</option>`).join('');
        }
    } catch (error) {
        console.error('Load playlists error:', error);
    }
}

async function addToSelectedPlaylist() {
    const playlistId = document.getElementById('playlistSelect').value;
    if (!playlistId || !selectedTrackId) {
        showNotification('Please select a playlist', 'error');
        return;
    }

    try {
        const response = await fetch(`${API_URL}/api/playlists/${playlistId}/tracks`, {
            method: 'POST',
            headers: {
                'Authorization': `Bearer ${authToken}`,
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ track_id: selectedTrackId })
        });

        if (response.ok) {
            showNotification('Track added to playlist!', 'success');
            closeModal('addToPlaylistModal');
        } else {
            const error = await response.json();
            showNotification(error.error || 'Failed to add track', 'error');
        }
    } catch (error) {
        console.error('Add to playlist error:', error);
        showNotification('Failed to add track', 'error');
    }
}

async function likeTrack(trackId) {
    try {
        const response = await fetch(`${API_URL}/api/tracks/${trackId}/like`, {
            method: 'POST',
            headers: { 'Authorization': `Bearer ${authToken}` }
        });

        if (response.ok) {
            const data = await response.json();
            showNotification(data.liked ? 'Liked!' : 'Unliked', 'success');
            if (currentPage === 'library') {
                loadTracks(currentPageNum);
            }
        }
    } catch (error) {
        console.error('Like error:', error);
    }
}

// Audio Player
function playTrack(trackId, trackName) {

    if (audioElement) {
        audioElement.pause();
    }

    currentTrack = {
        id: trackId,
        name: trackName
    };

    document.getElementById('currentTrack').textContent = trackName;

    document.getElementById('audioPlayer').style.display = 'block';

    audioElement = document.getElementById('mainAudio');

    if (!audioElement) {

        audioElement = document.createElement('audio');

        audioElement.id = 'mainAudio';

        audioElement.controls = true;

        audioElement.style.width = '100%';

        document.getElementById('audioPlayer').appendChild(audioElement);
    }

    audioElement.src = `${API_URL}/stream/${trackId}`;

    audioElement.load();

    audioElement.play()
        .then(() => {
            showNotification(`Now playing: ${trackName}`, 'success');
        })
        .catch(error => {
            console.error('Playback error:', error);
            showNotification('Playback failed', 'error');
        });
}

function togglePlay() {
    if (audioElement) {
        if (audioElement.paused) {
            audioElement.play();
        } else {
            audioElement.pause();
        }
    }
}

// Recommendations
async function loadRecommendations() {
    if (!authToken) return;

    try {
        const response = await fetch(`${API_URL}/api/recommendations?limit=6`, {
            headers: { 'Authorization': `Bearer ${authToken}` }
        });

        if (response.ok) {
            const data = await response.json();
            displayRecommendations(data.tracks || []);
        }
    } catch (error) {
        console.error('Load recommendations error:', error);
    }
}

function displayRecommendations(tracks) {
    const grid = document.getElementById('recommendationsGrid');
    if (!tracks.length) {
        grid.innerHTML = '<p>No recommendations yet. Start listening to get personalized recommendations!</p>';
        return;
    }

    grid.innerHTML = tracks.map(track => `
        <div class="track-card">
            <div class="track-cover">🎵</div>
            <div class="track-info">
                <h4>${escapeHtml(track.title)}</h4>
                <p>${escapeHtml(track.artist)}</p>
            </div>
            <button onclick="playTrack('${track.id}', '${escapeHtml(track.title)} - ${escapeHtml(track.artist)}')" class="btn-small">▶ Play</button>
        </div>
    `).join('');
}

// Pricing & Subscriptions
async function loadPricingPlans() {
    try {
        const response = await fetch(`${API_URL}/api/pricing-plans`);
        if (response.ok) {
            const data = await response.json();
            displayPricingPlans(data.plans || []);
        }
    } catch (error) {
        console.error('Load pricing error:', error);
    }
}

function displayPricingPlans(plans) {
    const grid = document.getElementById('pricingGrid');
    grid.innerHTML = plans.map(plan => `
        <div class="pricing-card">
            <h3>${plan.name}</h3>
            <div class="price">$${plan.price}<span>/${plan.interval}</span></div>
            <ul class="features">
                <li>✓ ${plan.quality}kbps quality</li>
                <li>${plan.offline_mode ? '✓' : '✗'} Offline mode</li>
                <li>✓ Unlimited skips</li>
                <li>✓ No ads</li>
            </ul>
            ${authToken ?
        `<button onclick="subscribe('${plan.id}', '${plan.name}', ${plan.price})" class="btn-primary">Subscribe</button>` :
        `<button onclick="showLogin()" class="btn-primary">Sign Up to Subscribe</button>`
    }
        </div>
    `).join('');
}

let selectedPlan = null;

function subscribe(planId, planName, price) {
    selectedPlan = { id: planId, name: planName, price: price };
    document.getElementById('planDetails').innerHTML = `
        <p><strong>${planName}</strong> - $${price}/month</p>
    `;
    document.getElementById('subscriptionModal').style.display = 'block';
}

async function processSubscription() {
    if (!selectedPlan) return;

    const cardNumber = document.getElementById('cardNumber').value;
    const cardExpiry = document.getElementById('cardExpiry').value;
    const cardCvv = document.getElementById('cardCvv').value;

    if (!cardNumber || !cardExpiry || !cardCvv) {
        showNotification('Please enter payment details', 'error');
        return;
    }

    try {
        const response = await fetch(`${API_URL}/api/subscription`, {
            method: 'POST',
            headers: {
                'Authorization': `Bearer ${authToken}`,
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({
                plan_id: selectedPlan.id,
                payment_method_id: 'card_' + Date.now()
            })
        });

        if (response.ok) {
            const data = await response.json();
            showNotification(`Subscribed to ${selectedPlan.name}!`, 'success');
            closeModal('subscriptionModal');

            // Process payment
            await fetch(`${API_URL}/api/payments/process`, {
                method: 'POST',
                headers: {
                    'Authorization': `Bearer ${authToken}`,
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify({
                    amount: selectedPlan.price,
                    currency: 'USD',
                    payment_method_id: 'card_' + Date.now(),
                    description: `${selectedPlan.name} subscription`
                })
            });
        } else {
            const error = await response.json();
            showNotification(error.error || 'Subscription failed', 'error');
        }
    } catch (error) {
        console.error('Subscription error:', error);
        showNotification('Subscription failed', 'error');
    }
}

// Utility Functions
function showNotification(message, type) {
    const notification = document.createElement('div');
    notification.className = `notification notification-${type}`;
    notification.textContent = message;
    notification.style.cssText = `
        position: fixed;
        top: 20px;
        right: 20px;
        padding: 12px 20px;
        border-radius: 8px;
        color: white;
        z-index: 10000;
        animation: slideIn 0.3s ease;
    `;
    notification.style.backgroundColor = type === 'success' ? '#4CAF50' : type === 'error' ? '#f44336' : '#2196F3';

    document.body.appendChild(notification);
    setTimeout(() => notification.remove(), 3000);
}

function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

function showUploadModal() {
    if (!authToken) {
        showLogin();
        return;
    }
    document.getElementById('uploadModal').style.display = 'block';
}

function showCreatePlaylistModal() {
    if (!authToken) {
        showLogin();
        return;
    }
    document.getElementById('playlistModal').style.display = 'block';
}

function closeModal(modalId) {
    document.getElementById(modalId).style.display = 'none';
}

// Close modals when clicking outside
window.onclick = function(event) {
    if (event.target.classList.contains('modal')) {
        event.target.style.display = 'none';
    }
}

// Load user playlists for navigation
async function loadUserPlaylists() {
    if (!authToken) return;
    try {
        const response = await fetch(`${API_URL}/api/playlists`, {
            headers: { 'Authorization': `Bearer ${authToken}` }
        });
        if (response.ok) {
            const data = await response.json();
            // Store for later use
            window.userPlaylists = data.playlists || [];
        }
    } catch (error) {
        console.error('Load user playlists error:', error);
    }
}

// Initialize
if (authToken) {
    loadUserPlaylists();
}