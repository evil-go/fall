package fall

import (
	"bufio"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
)

var components = map[reflect.Type][]*registerInfo{}
var values = map[string]interface{}{}
var names = map[string]*registerInfo{}

type registerInfo struct {
	I interface{}
	N string
}

type Initer interface {
	Init()
}

type InitLaster interface {
	InitLast()
}

func RegisterPropertiesFile(fileName string) {
	f, err := os.Open(fileName)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, "=")
		if len(parts) == 0 || len(parts) > 2 {
			panic("Invalid properties line " + line)
		}
		values[parts[0]] = parts[1]
	}
}

func RegisterValue(name string, value interface{}) {
	if _, ok := values[name]; ok {
		panic(fmt.Sprintf("Cannot register value %v for name %s because that name is already registered.", value, name))
	}
	values[name] = value
}

func Register(i interface{}) {
	// make sure i is a pointer
	if reflect.TypeOf(i).Kind() != reflect.Ptr {
		panic("Can only register pointers to things")
	}

	//build name for type
	t := reflect.TypeOf(i).Elem()
	s := t.PkgPath()
	if len(s) > 0 {
		s = s + "."
	}
	s = s + t.Name()
	if len(s) == 0 {
		panic("Cannot register type with no name: " + t.String())
	}
	RegisterName(i, s)
}

func RegisterName(i interface{}, name string) {
	// make sure i is a pointer
	if reflect.TypeOf(i).Kind() != reflect.Ptr {
		panic("Can only register pointers to things")
	}
	if _, ok := names[name]; ok {
		panic("Cannot register the same name twice: " + name)
	}
	t := reflect.TypeOf(i).Elem()
	info := &registerInfo{I: i, N: name}
	components[t] = append(components[t], info)
	names[name] = info
}

var regStackMap = map[string]bool{}
var regStack []string
var registered = map[string]bool{}

func processor(t reflect.Type, ri *registerInfo) {
	if regStackMap[ri.N] {
		panic(fmt.Sprintf("There's a cycle when wiring %s %v", ri.N, regStack))
	}
	regStack = append(regStack, ri.N)
	regStackMap[ri.N] = true

	//fmt.Println(ri.N, t.Kind())
	switch t.Kind() {
	case reflect.Struct:
		curVal := reflect.ValueOf(ri.I)
		numFields := t.NumField()
		for i := 0; i < numFields; i++ {

			f := t.Field(i)

			//fmt.Println(ri.N, f.Name)
			if val, ok := f.Tag.Lookup("value"); ok {
				//fmt.Println("found value tag for ", f)
				if v, ok := values[val]; !ok {
					panic(fmt.Sprintf("Unable to inject value named %s into field %s in %s because there is no value with that name", val, f.Name, ri.N))
				} else {
					vr := reflect.ValueOf(v)
					fv := curVal.Elem().Field(i)

					//if the value is a string and the field is an int, float, or bool, try to convert it
					if vr.Type().Kind() == reflect.String {
						var convVal interface{}
						var err error
						switch fv.Type().Kind() {
						case reflect.String:
							convVal = v
						case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
							convVal, err = strconv.ParseInt(v.(string), 10, fv.Type().Bits())
						case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
							convVal, err = strconv.ParseUint(v.(string), 10, fv.Type().Bits())
						case reflect.Bool:
							convVal, err = strconv.ParseBool(v.(string))
						case reflect.Float32, reflect.Float64:
							convVal, err = strconv.ParseFloat(v.(string), fv.Type().Bits())
						}
						if err != nil {
							panic(fmt.Sprintf("Unable to inject value named %s into field %s in %s because the value %s cannot be converted to a %s", val, f.Name, ri.N, v, fv.Type().String()))
						}
						fv.Set(reflect.ValueOf(convVal).Convert(fv.Type()))
						continue
					}

					if !vr.Type().ConvertibleTo(fv.Type()) {
						panic(fmt.Sprintf("Unable to inject value named %s into field %s in %s because the value %v of type %s is not convertable to a field of type %s", val, f.Name, ri.N, v, vr.Type().String(), fv.Type().String()))
					}
					fv.Set(vr.Convert(fv.Type()))
				}

				continue
			}

			ft := f.Type
			isPointer := false

			if ft.Kind() == reflect.Ptr {
				ft = ft.Elem()
				isPointer = true
			}
			var foundRi *registerInfo

			found := false
			// check to see if we can autowire this by name
			if autowire, ok := f.Tag.Lookup("name"); ok {
				foundRi, ok = names[autowire]
				if !ok {
					panic("Cannot autowire field " + f.Name + " in " + ri.N + " with " + autowire + " because it has not been registered")
				}
				found = true
			}

			// if there's not a wire tag, skip this field
			if _, ok := f.Tag.Lookup("wire"); !found && !ok {
				continue
			}

			// if not autowired by name, check to see if there's exactly one registered value of the specified type
			if foundManyRi, ok := components[ft]; !found && ok {
				if len(foundManyRi) > 1 {
					panic("Cannot autowire field " + f.Name + " in " + ri.N + " because there is more than one registered type of " + ft.PkgPath() + "." + ft.Name())
				}
				foundRi = foundManyRi[0]
				found = true
			}

			// check to see if this is an interface
			if !found && ft.Kind() != reflect.Interface {
				panic("Cannot autowire field " + f.Name + " in " + ri.N + " because there is nothing registered of type " + ft.PkgPath() + "." + ft.Name())
			}

			if !found {
				var allRi []*registerInfo
				// scan through every single registered thing and see if we can find something of the interface
				for inK, inVs := range components {
					if inK.Implements(ft) {
						allRi = append(allRi, inVs...)
					}
				}
				if len(allRi) == 0 {
					panic("Cannot autowire field " + f.Name + " in " + ri.N + " because there is nothing registered of type " + ft.PkgPath() + "." + ft.Name())
				}
				if len(allRi) > 1 {
					panic("Cannot autowire field " + f.Name + " in " + ri.N + " because there is more than one registered type of " + ft.PkgPath() + "." + ft.Name())
				}
				foundRi = allRi[0]
			}

			//check to see if foundRi has been registered.
			if !registered[foundRi.N] {
				//fmt.Println("initializing ",foundRi.N)
				riT := reflect.TypeOf(foundRi.I)
				if riT.Kind() == reflect.Ptr {
					riT = riT.Elem()
				}
				processor(riT, foundRi)
			}

			foundVal := reflect.ValueOf(foundRi.I)
			if !isPointer {
				foundVal = foundVal.Elem()
			}
			curVal.Elem().Field(i).Set(foundVal)
		}
	default:
		//todo
	}
	delete(regStackMap, ri.N)
	regStack = regStack[0 : len(regStack)-1]
	registered[ri.N] = true
	if ii, ok := (ri.I).(Initer); ok {
		ii.Init()
	}
}

func Start() {
	for k, v := range components {
		for _, curRi := range v {
			if registered[curRi.N] {
				continue
			}
			processor(k, curRi)
		}
	}
	for _, v := range components {
		for _, curRi := range v {
			if il, ok := curRi.I.(InitLaster); ok {
				il.InitLast()
			}
		}
	}
}

func Get(name string) interface{} {
	ri, ok := names[name]
	if !ok {
		return nil
	}
	return ri.I
}
