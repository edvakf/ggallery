Vue.filter('svg2url', function(svg) {
  if (!svg) {
    return "";
  } else {
    return window.URL.createObjectURL(new Blob([svg], {type: 'image/svg+xml'}));
  }
});

Vue.filter('filename2context', function(name) {
  if (/^[a-zA-Z0-9_]+$/.test(name)) {
    return 'has-success';
  } else {
    return 'has-error';
  }
});

var app = new Vue({
  el: '#app',
  data: {
    code: "",
    files: [],
    svg: "",
    output: "",
    error: "",
    id: "",
    is_loading: false,
  },
  ready: function() {
    if (/\/edit\/(\w+)/.test(location.pathname)) {
      this.id = RegExp.$1;
      getJSON('/plot/' + this.id, function(response) {
        if (response.files) {
          var i = 0;
          for (var name in response.files) {
            this.files.push({id: 'file' + i, name: name, content: response.files[name]});
            i++;
          }
        }
        this.code = response.code || "";
        this.output = response.output || "";
        this.error = response.error || "";

        $('#plotPanel').collapse(this.error ? 'hide' : 'show');
        $('#outputPanel').collapse(this.error ? 'show' : 'hide');

        if (!this.error) {
          this.$options.methods.run.call(this);
        }
      }.bind(this));
    }
  },
  computed: {
    imageUrl: function() {
      return urlBase() + '/plot/' + this.id + '.svg';
    },
    editUrl: function() {
      return urlBase() + '/edit/' + this.id;
    },
    htmlImage: function() {
      return '<img src="' + urlBase() + '/plot/' + this.id + '.svg' + '">';
    },
    replotApi: function() {
      if (this.files.length == 0) {
        return '';
      }
      var filemap = {};
      this.files.forEach(function(file) { filemap[file.name] = 'content of ' + file.name + '' });
      return 'curl -H \'Content-Type: application/json\' --data-binary \'' + JSON.stringify(filemap) + '\' '
        + urlBase() + '/replot/' + this.id;
    },
  },
  methods: {
    run: function() {
      this.is_loading = true;
      run(this.code, this.files, function(response) {
        this.is_loading = false;
        this.output = response.output || "";
        this.svg = response.svg || "";
        this.error = response.error || "";

        $('#plotPanel').collapse(this.error ? 'hide' : 'show');
        $('#outputPanel').collapse(this.error ? 'show' : 'hide');
      }.bind(this));
    },
    save: function() {
      this.is_loading = true;
      plot(this.code, this.files, function(response) {
        this.is_loading = false;
        this.output = response.output || "";
        this.svg = response.svg || "";
        this.error = response.error || "";
        this.id = response.id || "";

        $('#plotPanel').collapse(this.error ? 'hide' : 'show');
        $('#outputPanel').collapse(this.error ? 'show' : 'hide');

        history.pushState(null, "", '/edit/' + this.id);
      }.bind(this));
    },
    addFile: function() {
      var i = 1;
      while (true) {
        var name = "file" + i;
        if (!this.files.some(function(f) {return f.id === name})) {
          // `id` here is used only on the client side for deleting the tab.
          this.files.push({id: name, name: name, content: ""});
          setTimeout(function() {
            $('#' + name + '-tab').tab('show');
          }, 50);
          break;
        }
        i++;
      }
    },
    removeFile: function(ev) {
      var id = ev.target.id;
      for (var i = 0; i < this.files.length; i++) {
        if (this.files[i].id === id && window.confirm('Are you sure to remove this file?')) {
          this.files.splice(i, 1);
          $('#code-tab').tab('show');
          break;
        }
      }
    }
  }
});

function run(code, files, callback) {
  var filemap = {};
  files.forEach(function(file) { filemap[file.name] = file.content });
  postJSON('/run', {code: code, files: filemap}, callback);
}

function plot(code, files, callback) {
  var filemap = {};
  files.forEach(function(file) { filemap[file.name] = file.content });
  postJSON('/plot', {code: code, files: filemap}, callback);
}

function postJSON(endpoint, obj, callback) {
  var xhr = new XMLHttpRequest;
  xhr.open('POST', endpoint, true);
  xhr.setRequestHeader('Content-Type', 'application/json');
  xhr.onload = function() { callback(JSON.parse(xhr.responseText)) };
  xhr.send(JSON.stringify(obj));
}

function getJSON(endpoint, callback) {
  var xhr = new XMLHttpRequest;
  xhr.open('GET', endpoint, true);
  xhr.onload = function() { callback(JSON.parse(xhr.responseText)) };
  xhr.send(null);
}

function urlBase() {
  return location.protocol + '//' + location.host;
}
