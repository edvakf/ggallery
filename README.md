# ggallery

Make a cool plot with R and ggplot2

## API

* POST /run

run an R code then return R's output and svg image

```
post body: {code:string, files:{name1:content1, name2:content2}}
ok response: {output:string, svg:string}
error response: {output:string, message:string}
```

* POST /plot

make a plot and save the result for later reference (ie. give an id)

```
post body: {code:string, files:{name1:content1, name2:content2}}
ok response: {output:string, svg:string, id:string, svg_url:string}
error response: {error:string, output:string}
```

* POST /replot

make a plot again with different data set

```
post body: {id:string, files:{name1:content1, name2:content2}}
ok response: {output:string, svg:string, id:string, svg_url:string}
error response: {error:string, output:string}
```

* GET /plot/:id.svg

run a saved code and show svg image

* GET /plot/:id

return saved code and files

```
response: {code:string, files:{name1:content1, name2:content2}}
error response: {error:string, output:string}
```
