package v1

import "net/http"

func Web() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		html := `
<!DOCTYPE html>
<html>
<head>
    <title>File Upload</title>
    <style>
        form {
            margin: 20px;
        }
        .form-group {
            margin-bottom: 10px;
        }
    </style>
</head>
<body>
    <form id="uploadForm" onsubmit="uploadFile(event)">
        <div class="form-group">
            <label for="fileInput">Select file:</label>
            <input type="file" id="fileInput" required>
        </div>
        <div class="form-group">
            <input type="submit" value="Upload File">
        </div>
    </form>

    <script>
    function uploadFile(event) {
        event.preventDefault();
        
        const fileInput = document.getElementById('fileInput');
        const file = fileInput.files[0];
        
        if (!file) {
            alert('Please select a file first');
            return;
        }

        fetch('/api/v1/binary', {
            method: 'POST',
            body: file,
            headers: {
                'X-Api-File-Name': file.name
            }
        })
        .then(response => {
            if (response.ok) {
                alert('File uploaded successfully');
                document.getElementById('uploadForm').reset();
            } else {
                alert('Upload failed');
            }
        })
        .catch(error => {
            console.error('Error:', error);
            alert('Upload failed');
        });
    }
    </script>
</body>
</html>`

		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
	}
}