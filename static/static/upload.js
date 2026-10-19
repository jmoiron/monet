/**
 * jQuery Upload Plugin
 * Converts any element into a drag & drop file upload area
 */
$(() => {
    'use strict';

    $.fn.upload = function(options) {
        // Default options
        const defaults = {
            url: '/admin/uploads/upload',
            multiple: true,
            accept: '*',
            maxFileSize: 64 * 1024 * 1024, // 64MB
            dragOverClass: 'drag-over',
            // overlay: the element is hidden until a file is dragged into the
            // window, when it covers the page and the whole window takes the drop
            overlay: false,
            trigger: null, // a selector for something that opens the file chooser
            progressContainer: null, // Auto-create if null
            onSuccess: function(response, filename) {
                console.log('Upload successful:', filename, response);
            },
            onError: function(error, filename) {
                console.error('Upload failed:', filename, error);
            },
            onComplete: function(successful, failed) {
                if (successful > 0) {
                    // Default: reload page after successful uploads
                    setTimeout(() => {
                        window.location.reload();
                    }, 1500);
                }
            },
            text: {
                dropZone: 'Drag & drop files here or click to upload',
                hint: 'Select files to upload',
                uploading: 'Uploading...',
                processing: 'Processing...',
                complete: 'Complete',
                failed: 'Failed',
                error: 'Error'
            },
            icons: {
                upload: 'fa-solid fa-cloud-arrow-up',
                file: 'fa-solid fa-file'
            }
        };

        const settings = $.extend(true, {}, defaults, options);

        return this.each(function() {
            const $container = $(this);
            let $progressContainer = null;
            let uploadCounter = 0;

            // set when the window is being watched for files being dragged in
            let endWindowDrag = () => {};

            // Initialize the upload area
            init();

            function init() {
                setupDropZone();
                setupProgressContainer();
                bindEvents();
                if (settings.overlay) {
                    bindWindowDrag();
                }
            }

            function setupDropZone() {
                if (settings.overlay) {
                    $container.addClass('upload-overlay');
                } else {
                    $container.addClass('upload-drop-zone');
                }
                $container.html(`<div class="${settings.overlay ? 'upload-drop-zone upload-overlay-box' : 'upload-zone-content'}">
                    <div class="upload-icon">
                        <i class="${settings.icons.upload}"></i>
                    </div>
                    <div class="upload-text">${settings.text.dropZone}</div>
                    <div class="upload-hint">${settings.text.hint}</div>
                    <input type="file" class="upload-file-input" ${settings.multiple ? 'multiple' : ''} ${settings.accept !== '*' ? 'accept="' + settings.accept + '"' : ''} style="display: none;">
                </div>`);
            }

            function setupProgressContainer() {
                if (settings.progressContainer) {
                    $progressContainer = $(settings.progressContainer);
                } else {
                    // Create progress container after the drop zone
                    $progressContainer = $('<div class="upload-progress" style="display: none;"><h3>Uploading Files</h3><div class="upload-progress-list"></div></div>');
                    $container.after($progressContainer);
                }
            }

            function bindEvents() {
                const $fileInput = $container.find('input.upload-file-input');

                // Click to upload
                if (!settings.overlay) {
                    $container.on('click', function(e) {
                        if (!$(e.target).is('input[type="file"]')) {
                            $fileInput.trigger("click");
                        }
                    });
                }
                if (settings.trigger) {
                    $(settings.trigger).on('click', function(e) {
                        e.preventDefault();
                        $fileInput.trigger("click");
                    });
                }

                // File input change
                $fileInput.on('change', function(e) {
                    const files = Array.from(e.target.files);
                    uploadFiles(files);
                    e.target.value = ''; // Clear input
                });

                // Drag & drop events
                // (an overlay lets these through: the window is counting them)
                $container.on('dragover', function(e) {
                    e.preventDefault();
                    if (!settings.overlay) e.stopPropagation();
                    $container.addClass(settings.dragOverClass);
                });

                $container.on('dragleave', function(e) {
                    e.preventDefault();
                    if (!settings.overlay) e.stopPropagation();
                    // Only remove if leaving the container itself
                    if (!$container[0].contains(e.relatedTarget)) {
                        $container.removeClass(settings.dragOverClass);
                    }
                });

                $container.on('drop', function(e) {
                    e.preventDefault();
                    e.stopPropagation();
                    $container.removeClass(settings.dragOverClass);
                    endWindowDrag();

                    const files = Array.from(e.dataTransfer.files);
                    uploadFiles(files);
                });
            }

            // Shows the overlay while files are being dragged anywhere over the
            // window. Drags of text or links are not files, and are left alone.
            function bindWindowDrag() {
                let depth = 0;
                const hasFiles = e => e.dataTransfer && Array.from(e.dataTransfer.types || []).includes('Files');
                const show = on => $container.toggleClass('active', on);

                endWindowDrag = () => { depth = 0; show(false); };

                document.addEventListener('dragenter', e => {
                    if (!hasFiles(e)) return;
                    depth++;
                    show(true);
                });
                document.addEventListener('dragleave', e => {
                    if (!hasFiles(e)) return;
                    depth = Math.max(0, depth - 1);
                    if (depth === 0) show(false);
                });
                document.addEventListener('dragover', e => {
                    if (hasFiles(e)) e.preventDefault();
                });
                // a drop that misses the overlay must not open the file in the tab
                document.addEventListener('drop', e => {
                    if (hasFiles(e)) e.preventDefault();
                    endWindowDrag();
                });
            }

            function uploadFiles(files) {
                if (files.length === 0) return;

                // Filter files by size
                const validFiles = files.filter(file => {
                    if (file.size > settings.maxFileSize) {
                        settings.onError('File too large: ' + file.name, file.name);
                        return false;
                    }
                    return true;
                });

                if (validFiles.length === 0) return;

                // Show progress container
                $progressContainer.show();
                const $progressList = $progressContainer.find('.upload-progress-list');
                if ($progressList.length === 0) {
                    $progressContainer.append('<div class="upload-progress-list"></div>');
                }

                let successful = 0;
                let failed = 0;
                const totalFiles = validFiles.length;

                validFiles.forEach(file => {
                    const uploadId = ++uploadCounter;
                    const $progressItem = createProgressItem(file.name, uploadId);
                    $progressContainer.find('.upload-progress-list').append($progressItem);

                    uploadFile(file, uploadId)
                        .then(response => {
                            updateProgressItem(uploadId, 100, 'success', response.message || settings.text.complete);
                            successful++;
                            settings.onSuccess(response, file.name);
                        })
                        .catch(error => {
                            updateProgressItem(uploadId, 0, 'error',
                                error && error.message ? settings.text.error + ': ' + error.message : settings.text.error);
                            failed++;
                            settings.onError(error, file.name);
                        })
                        .finally(() => {
                            // Check if all uploads are complete
                            if (successful + failed === totalFiles) {
                                settings.onComplete(successful, failed);
                            }
                        });
                });
            }

            function uploadFile(file, uploadId) {
                return new Promise((resolve, reject) => {
                    const formData = new FormData();
                    formData.append('file', file);

                    const xhr = new XMLHttpRequest();

                    xhr.upload.addEventListener('progress', function(e) {
                        if (e.lengthComputable) {
                            const percentComplete = (e.loaded / e.total) * 100;
                            updateProgressItem(uploadId, percentComplete, '', settings.text.uploading + ' ' + Math.round(percentComplete) + '%');
                        }
                    });

                    // the file is all there; the server may take a while to do something with it
                    xhr.upload.addEventListener('load', function() {
                        updateProgressItem(uploadId, 100, '', settings.text.processing);
                    });

                    xhr.addEventListener('load', function() {
                        if (xhr.status >= 200 && xhr.status < 300) {
                            try {
                                const response = JSON.parse(xhr.responseText);
                                if (response.success) {
                                    resolve(response);
                                } else {
                                    reject(new Error(response.error || 'Upload failed'));
                                }
                            } catch (e) {
                                reject(new Error('Invalid response format'));
                            }
                        } else {
                            // a handler that says why (as json) is believed over the bare status
                            let reason = '';
                            try { reason = JSON.parse(xhr.responseText).error || ''; } catch (e) {}
                            reject(new Error(reason || 'HTTP ' + xhr.status + ': ' + xhr.statusText));
                        }
                    });

                    xhr.addEventListener('error', function() {
                        reject(new Error('Network error'));
                    });

                    xhr.open('POST', settings.url);
                    xhr.send(formData);
                });
            }

            function createProgressItem(filename, uploadId) {
                return $(`
                    <div class="progress-item" data-upload-id="${uploadId}">
                        <div class="filename">
                            <i class="${settings.icons.file}"></i>
                            ${filename}
                        </div>
                        <div class="progress-bar">
                            <div class="progress-fill" style="width: 0%"></div>
                        </div>
                        <div class="upload-status">${settings.text.uploading}...</div>
                    </div>
                `);
            }

            function updateProgressItem(uploadId, progress, statusClass, statusText) {
                const $item = $progressContainer.find(`[data-upload-id="${uploadId}"]`);
                if ($item.length === 0) return;

                const $progressFill = $item.find('.progress-fill');
                const $statusElement = $item.find('.upload-status');

                $progressFill.css('width', progress + '%');
                $statusElement.text(statusText);

                if (statusClass) {
                    $statusElement.removeClass('success error').addClass(statusClass);
                }
            }
        });
    };

    // Static methods for utility functions
    $.fn.upload.humanizeBytes = function(bytes) {
        if (bytes === 0) return '0 B';
        const k = 1024;
        const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
        const i = Math.floor(Math.log(bytes) / Math.log(k));
        return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
    };
});