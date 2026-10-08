module.exports = {
	// Scan both editable Templ files and generated Go templates for classes.
	content: ["./views/**/*.templ", "./views/**/*_templ.go"],
	theme: {
		extend: {},
	},
	plugins: [],
};
