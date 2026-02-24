/**
 * Confirmation dialog JavaScript for write-action approvals.
 *
 * Provides Alpine.js component and WebSocket handling for displaying
 * and responding to write-action confirmation requests.
 */

// Global WebSocket connection for write-action confirmations
window.writeActionWebSocket = null;

/**
 * Initialize WebSocket connection to write-action endpoint.
 * @returns {Promise<WebSocket>} Connected WebSocket instance
 */
function initWriteActionWebSocket() {
    if (window.writeActionWebSocket && window.writeActionWebSocket.readyState === WebSocket.OPEN) {
        return Promise.resolve(window.writeActionWebSocket);
    }

    return new Promise((resolve, reject) => {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${protocol}//${window.location.host}/ws/write-action`;

        const ws = new WebSocket(wsUrl);
        window.writeActionWebSocket = ws;

        ws.onopen = () => {
            console.log('Write-action WebSocket connected');
            resolve(ws);
        };

        ws.onerror = (error) => {
            console.error('Write-action WebSocket error:', error);
            reject(error);
        };

        ws.onclose = () => {
            console.log('Write-action WebSocket disconnected');
            window.writeActionWebSocket = null;
        };

        // Handle incoming messages
        ws.onmessage = (event) => {
            try {
                const data = JSON.parse(event.data);
                handleWebSocketMessage(data);
            } catch (error) {
                console.error('Failed to parse WebSocket message:', error);
            }
        };
    });
}

/**
 * Handle incoming WebSocket messages.
 * @param {Object} data - Parsed message data
 */
function handleWebSocketMessage(data) {
    if (data.type === 'write_action_request') {
        // Show confirmation dialog
        showConfirmationDialog(data);
    } else if (data.type === 'error') {
        console.error('WebSocket error:', data.content);
        alert(`WebSocket Error: ${data.content}`);
    }
    // Other message types are ignored
}

/**
 * Show confirmation dialog with request data.
 * @param {Object} requestData - WriteActionRequest data
 */
function showConfirmationDialog(requestData) {
    // Ensure Alpine.js is loaded
    if (typeof Alpine === 'undefined') {
        console.error('Alpine.js not loaded');
        alert('Cannot show confirmation dialog: Alpine.js not loaded');
        return;
    }

    // Get Alpine component instance
    const modalElement = document.getElementById('write-action-confirmation-modal');
    if (!modalElement) {
        console.error('Confirmation dialog element not found');
        return;
    }

    // Use Alpine.$data to access component data
    const component = Alpine.$data(modalElement);
    if (component && typeof component.show === 'function') {
        component.show(requestData);
    } else {
        console.error('Alpine component not initialized or missing show method');
    }
}

/**
 * Send write-action response to server.
 * @param {string} requestId - Request ID
 * @param {boolean} approved - Whether the operation is approved
 */
function sendWriteActionResponse(requestId, approved) {
    if (!window.writeActionWebSocket || window.writeActionWebSocket.readyState !== WebSocket.OPEN) {
        console.error('WebSocket not connected');
        alert('Cannot send response: WebSocket disconnected. Please refresh the page.');
        return;
    }

    const response = {
        type: 'write_action_response',
        id: requestId,
        approved: approved,
        timestamp: new Date().toISOString()
    };

    window.writeActionWebSocket.send(JSON.stringify(response));
    console.log('Sent write-action response:', response);
}

// Initialize Alpine.js component
document.addEventListener('alpine:init', () => {
    Alpine.data('writeActionConfirmation', () => ({
        // Request data
        requestId: '',
        agentId: '',
        agentColor: '#6c757d', // default gray
        operation: '',
        filePath: '',
        timestamp: '',

        // UI state
        rememberChoice: false,
        title: 'Write Action Confirmation',

        // Computed properties
        get operationLabel() {
            const ops = { create: 'Create', update: 'Update', delete: 'Delete' };
            return ops[this.operation] || this.operation;
        },

        get operationClass() {
            const classes = {
                create: 'bg-success text-white',
                update: 'bg-warning text-dark',
                delete: 'bg-danger text-white'
            };
            return classes[this.operation] || 'bg-secondary text-white';
        },

        // Show modal with request data
        show(requestData) {
            this.requestId = requestData.id;
            this.agentId = requestData.agent_id;
            this.operation = requestData.operation;
            this.filePath = requestData.file_path;
            this.timestamp = new Date(requestData.timestamp).toLocaleString();

            // Set agent color if provided in request
            if (requestData.agent_color) {
                this.agentColor = requestData.agent_color;
            }

            // Show modal
            const modal = new bootstrap.Modal(document.getElementById('write-action-confirmation-modal'));
            modal.show();
        },

        // Send approval response
        approve() {
            this.sendResponse(true);
        },

        // Send rejection response
        reject() {
            this.sendResponse(false);
        },

        // Send response via WebSocket
        sendResponse(approved) {
            sendWriteActionResponse(this.requestId, approved);

            // If remember choice is checked, store preference
            if (this.rememberChoice) {
                localStorage.setItem(`write-action-preference-${this.agentId}-${this.operation}`, approved);
            }
        }
    }));
});

// Auto-initialize WebSocket connection when page loads
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', () => {
        initWriteActionWebSocket().catch(error => {
            console.warn('Failed to connect write-action WebSocket:', error);
        });
    });
} else {
    initWriteActionWebSocket().catch(error => {
        console.warn('Failed to connect write-action WebSocket:', error);
    });
}